package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/constants"
	"github.com/mxwell/qarau/internal/quota"
)

type JobService struct {
	log     *slog.Logger
	queries *dbgen.Queries
	pool    *pgxpool.Pool
}

func NewService(log *slog.Logger, queries *dbgen.Queries, pool *pgxpool.Pool) (*JobService, error) {
	if log == nil {
		return nil, errors.New("nil logger in JobService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in JobService creation")
	}
	if pool == nil {
		return nil, errors.New("nil pool in JobService creation")
	}
	return &JobService{
		log:     log,
		queries: queries,
		pool:    pool,
	}, nil
}

func convertJobType(protoJobType qarauv1.JobType) (dbgen.JobType, error) {
	switch protoJobType {
	case qarauv1.JobType_JOB_TYPE_ASR:
		return dbgen.JobTypeAsr, nil
	case qarauv1.JobType_JOB_TYPE_FETCH:
		return dbgen.JobTypeFetch, nil
	default:
		return "", fmt.Errorf("unknown job type %v", protoJobType)
	}
}

func prepareJobPayload(jobType qarauv1.JobType, row dbgen.ClaimJobRow) (*qarauv1.JobPayload, error) {
	switch jobType {
	case qarauv1.JobType_JOB_TYPE_FETCH:
		return &qarauv1.JobPayload{
			Payload: &qarauv1.JobPayload_FetchJob{
				FetchJob: &qarauv1.FetchJob{
					OnlineVideoId: row.OnlineVideoID,
				},
			},
		}, nil
	case qarauv1.JobType_JOB_TYPE_ASR:
		return &qarauv1.JobPayload{
			Payload: &qarauv1.JobPayload_AsrJob{
				AsrJob: &qarauv1.AsrJob{},
			},
		}, nil
	default:
		return nil, fmt.Errorf("payload for job type %v not supported", jobType)
	}
}

func (s *JobService) checkQuotaOk(ctx context.Context) (bool, error) {
	usedSeconds, err := s.queries.GetAsrQuota(ctx, quota.GetTodayForQuota())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		s.log.Error("failed to get asr quota", "err", err)
		return false, err
	}
	ok := usedSeconds < constants.AsrApiDailyQuotaSeconds
	if !ok {
		s.log.Info("daily asr api quota exhausted", "used", usedSeconds, "quota", constants.AsrApiDailyQuotaSeconds)
	}
	return ok, nil
}

func (s *JobService) LeaseJob(ctx context.Context, workerID string, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	jobType, err := convertJobType(request.Type)
	if err != nil {
		return nil, err
	}

	lockPeriod := 5 * time.Minute
	if jobType == dbgen.JobTypeAsr {
		lockPeriod = 60 * time.Minute
	}

	if jobType == dbgen.JobTypeAsr {
		ok, err := s.checkQuotaOk(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &qarauv1.LeaseJobResponse{}, nil
		}
	}

	lockedUntil := pgtype.Timestamptz{
		Time:  time.Now().Add(lockPeriod),
		Valid: true,
	}
	arg := dbgen.ClaimJobParams{
		LockedBy:    &workerID,
		LockedUntil: lockedUntil,
		JobType:     jobType,
	}
	row, err := s.queries.ClaimJob(ctx, arg)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &qarauv1.LeaseJobResponse{}, nil
		}
		return nil, err
	}
	payload, err := prepareJobPayload(request.Type, row)
	if err != nil {
		return nil, err
	}

	return &qarauv1.LeaseJobResponse{
		Job: &qarauv1.Job{
			Id:      row.ID,
			VideoId: row.VideoID,
			Type:    request.Type,
			Payload: payload,
		},
	}, nil
}

/*
 * The method does 3 things under 1 transaction: creates a new audio blob, marks the fetch job done and creates a new ASR job
 */
func (s *JobService) CompleteFetchJob(ctx context.Context, workerID string, jobID int64, videoID int64, audio []byte, filename string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe even after tx.Commit() -- does nothing

	qtx := s.queries.WithTx(tx)

	_, err = qtx.CreateAudioBlob(ctx, dbgen.CreateAudioBlobParams{
		VideoID:  videoID,
		Content:  audio,
		Filename: filename,
	})
	if err != nil {
		s.log.Error("failed to create audio blob", "job", jobID, "err", err)
		return err
	}
	_, err = qtx.MarkJobDone(ctx, dbgen.MarkJobDoneParams{
		JobID:    jobID,
		JobType:  dbgen.JobTypeFetch,
		LockedBy: &workerID,
	})
	if err != nil {
		s.log.Error("failed to mark fetch job done", "job", jobID, "err", err)
		return err
	}
	_, err = qtx.CreateAsrJob(ctx, jobID)
	if err != nil {
		s.log.Error("failed to create ASR job", "job", jobID, "err", err)
		return err
	}

	return tx.Commit(ctx)
}

/*
 * Check that the job is actually locked by the given worker and the lock hasn't expired
 */
func (s *JobService) CheckLease(ctx context.Context, jobID int64, workerID string) bool {
	row, err := s.queries.CheckLease(ctx, jobID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to check lease in DB", "job", jobID, "err", err)
		}
		return false
	}
	if *row.LockedBy != workerID {
		s.log.Info("lease owner mismatch", "job", jobID, "locked_by", *row.LockedBy, "workerID", workerID)
		return false
	}
	if row.LockedUntil.Time.Before(time.Now()) {
		s.log.Info("lease expired", "job", jobID, "locked_until", row.LockedUntil.Time)
		return false
	}
	if row.State != dbgen.JobStateRunning {
		s.log.Info("lease not valid as job not running", "job", jobID, "state", row.State)
		return false
	}
	return true
}

type AudioBlobContent struct {
	Filename     string
	Content      []byte
	DurationSecs int32
}

func (s *JobService) GetAudioBlob(ctx context.Context, videoID int64) (AudioBlobContent, error) {
	row, err := s.queries.GetAudioBlob(ctx, videoID)
	if err != nil {
		s.log.Error("failed to load audio blob", "videoID", videoID, "err", err)
		return AudioBlobContent{}, err
	}
	video, err := s.queries.GetVideoByID(ctx, videoID)
	if err != nil {
		s.log.Error("failed to load video", "videoID", videoID, "err", err)
		return AudioBlobContent{}, err
	}
	return AudioBlobContent{
		Filename:     row.Filename,
		Content:      row.Content,
		DurationSecs: int32(video.Duration.Microseconds / microsecondsPerSecond),
	}, nil
}

func (s *JobService) getVideoDuration(ctx context.Context, videoID int64) (int32, error) {
	video, err := s.queries.GetVideoByID(ctx, videoID)
	if err != nil {
		return 0, err
	}
	return int32((video.Duration.Microseconds + microsecondsPerSecond - 1) / microsecondsPerSecond), nil
}

/*
 * The method does several things under 1 transaction:
 * - creates a new transription (or replaces old transcription by the same model)
 * - ensures old words are removed in the case of existing transcription replacement
 * - inserts new words
 * - marks the ASR job done
 */
func (s *JobService) CompleteAsrJob(ctx context.Context, workerID string, jobID int64, videoID int64, transcription *qarauv1.Transcription) error {
	usedSeconds, err := s.getVideoDuration(ctx, videoID)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe even after tx.Commit() -- does nothing

	qtx := s.queries.WithTx(tx)

	transcriptionID, err := qtx.UpsertTranscription(ctx, dbgen.UpsertTranscriptionParams{
		VideoID: videoID,
		Model:   transcription.Model,
	})
	if err != nil {
		s.log.Error("failed to upsert transcription", "job", jobID, "err", err)
		return err
	}

	err = qtx.DeleteWordsByTranscriptionId(ctx, transcriptionID)
	if err != nil {
		s.log.Error("word delete request failed", "job", jobID, "err", err)
		return err
	}

	if len(transcription.Words) > 0 {
		wordsParams := make([]dbgen.InsertWordsParams, 0, len(transcription.Words))
		for i := range transcription.Words {
			word := transcription.Words[i]
			wordsParams = append(wordsParams, dbgen.InsertWordsParams{
				TranscriptionID: transcriptionID,
				Seq:             int32(i),
				StartMs:         int32(word.StartMs),
				EndMs:           int32(word.EndMs),
				Word:            word.Word,
				Confidence:      int16(word.Confidence),
				Speaker:         int32(word.Speaker),
			})
		}
		insertCount, err := qtx.InsertWords(ctx, wordsParams)
		if err != nil {
			s.log.Error("word bulk insert failed", "job", jobID, "count", len(transcription.Words), "err", err)
			return err
		}
		s.log.Info("inserted words", "job", jobID, "transcription", transcriptionID, "insertCount", insertCount)
	} else {
		s.log.Info("no words inserted", "job", jobID, "transcription", transcriptionID)
	}

	_, err = qtx.MarkJobDone(ctx, dbgen.MarkJobDoneParams{
		JobID:    jobID,
		JobType:  dbgen.JobTypeAsr,
		LockedBy: &workerID,
	})
	if err != nil {
		s.log.Error("failed to mark ASR job done", "job", jobID, "err", err)
		return err
	}

	_, err = qtx.UpsertAsrQuota(ctx, dbgen.UpsertAsrQuotaParams{
		Day:         quota.GetTodayForQuota(),
		UsedSeconds: usedSeconds,
	})
	if err != nil {
		s.log.Error("failed to upsert ASR quota", "job", jobID, "err", err)
		return err
	}

	s.log.Info("transcription is stored to DB", "job", jobID, "transcription", transcriptionID, "words", len(transcription.Words), "used_secs", usedSeconds)
	return tx.Commit(ctx)
}

func (s *JobService) FailJob(ctx context.Context, jobID int64, workerID string, errorMessage string, final bool) error {
	job, err := s.queries.GetJob(ctx, jobID)
	if err != nil {
		s.log.Error("failed to load job to mark failed", "job", jobID, "err", err)
		return err
	}
	if job.State != "running" {
		s.log.Error("unexpected job state", "job", jobID, "state", job.State)
		return errors.New("unexpected job state")
	}
	if job.LockedBy == nil || *job.LockedBy != workerID {
		s.log.Error("job not locked by worker", "job", jobID, "locked_by", job.LockedBy, "worker", workerID)
		return errors.New("this worker not allowed to modify the job")
	}
	if job.Attempts < job.MaxAttempts && !final {
		_, err = s.queries.UnlockJob(ctx, dbgen.UnlockJobParams{
			ErrorMessage: &errorMessage,
			JobID:        jobID,
			LockedBy:     &workerID,
		})
		if err != nil {
			s.log.Error("failed to unlock job", "job", jobID, "worker", workerID)
			return err
		}
		s.log.Info("job unlocked by worker", "job", jobID, "worker", workerID)
	} else {
		_, err = s.queries.MarkJobFailed(ctx, dbgen.MarkJobFailedParams{
			ErrorMessage: &errorMessage,
			JobID:        jobID,
			LockedBy:     &workerID,
		})
		if err != nil {
			s.log.Error("failed to mark job failed", "job", jobID, "worker", workerID)
			return err
		}
		s.log.Info("job marked failed", "job", jobID, "worker", workerID)
	}
	return nil
}
