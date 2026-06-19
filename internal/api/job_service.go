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
	if jobType == qarauv1.JobType_JOB_TYPE_FETCH {
		return &qarauv1.JobPayload{
			Payload: &qarauv1.JobPayload_FetchJob{
				FetchJob: &qarauv1.FetchJob{
					OnlineVideoId: row.OnlineVideoID,
				},
			},
		}, nil
	}
	// TODO support ASR jobs
	return nil, fmt.Errorf("payload for job type %v not supported", jobType)
}

func (s *JobService) LeaseJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	if len(request.WorkerId) == 0 {
		return nil, errors.New("empty worker ID")
	}

	// TODO support ASR jobs
	if request.Type != qarauv1.JobType_JOB_TYPE_FETCH {
		return nil, errors.ErrUnsupported
	}

	jobType, err := convertJobType(request.Type)
	if err != nil {
		return nil, err
	}

	lockPeriod := 5 * time.Minute
	if jobType == dbgen.JobTypeAsr {
		lockPeriod = 15 * time.Minute
	}

	lockedUntil := pgtype.Timestamptz{
		Time:  time.Now().Add(lockPeriod),
		Valid: true,
	}
	arg := dbgen.ClaimJobParams{
		LockedBy:    &request.WorkerId,
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
func (s *JobService) CompleteFetchJob(ctx context.Context, workerID string, jobID int64, audio []byte, filename string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe even after tx.Commit() -- does nothing

	qtx := s.queries.WithTx(tx)

	_, err = qtx.CreateAudioBlob(ctx, dbgen.CreateAudioBlobParams{
		JobID:    jobID,
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
