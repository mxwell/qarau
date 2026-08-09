package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/quota"
	"github.com/mxwell/qarau/internal/subtitles"
)

var (
	ErrCorruptedData       = errors.New("corrupted data")
	ErrUnprocessableVideo  = errors.New("unprocessable video")
	ErrNoSuchTranscription = errors.New("no transcription")
	ErrNoSuchSeq           = errors.New("no seq")
)

type VideoService struct {
	log      *slog.Logger
	queries  *dbgen.Queries
	ytClient *YtClient
}

func NewVideoService(log *slog.Logger, queries *dbgen.Queries, ytClient *YtClient) (*VideoService, error) {
	if log == nil {
		return nil, errors.New("nil logger in VideoService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in VideoService creation")
	}
	if ytClient == nil {
		return nil, errors.New("nil YT client in VideoService creation")
	}
	return &VideoService{
		log:      log,
		queries:  queries,
		ytClient: ytClient,
	}, nil
}

const (
	maxQueueSize = 5
	seqBeyondEnd = math.MaxInt32
)

func pgIntervalToInt32Seconds(interval *pgtype.Interval) int32 {
	return MicrosToFloorInt32Seconds(interval.Microseconds)
}

func rowToVideo(row *dbgen.GetVideoRow) (*Video, error) {
	if row.DefaultLang == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailUrl == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailWidth == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailHeight == nil {
		return nil, ErrCorruptedData
	}
	return &Video{
		ID:              row.ID,
		OnlineVideoID:   row.OnlineVideoID,
		Title:           row.Title,
		ChannelID:       row.ChannelID,
		ChannelTitle:    row.ChannelTitle,
		PublishedAt:     row.PublishedAt.Time,
		DurationSecs:    pgIntervalToInt32Seconds(&row.Duration),
		Views:           row.Views,
		Likes:           row.Likes,
		DefaultLang:     *row.DefaultLang,
		Embeddable:      row.Embeddable,
		ThumbnailURL:    *row.ThumbnailUrl,
		ThumbnailWidth:  *row.ThumbnailWidth,
		ThumbnailHeight: *row.ThumbnailHeight,
		LoadedFromDB:    true,
	}, nil
}

func byIDRowToVideo(row *dbgen.GetVideoByIDRow) (*Video, error) {
	if row.DefaultLang == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailUrl == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailWidth == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailHeight == nil {
		return nil, ErrCorruptedData
	}
	return &Video{
		ID:              row.ID,
		OnlineVideoID:   row.OnlineVideoID,
		Title:           row.Title,
		ChannelID:       row.ChannelID,
		ChannelTitle:    row.ChannelTitle,
		PublishedAt:     row.PublishedAt.Time,
		DurationSecs:    pgIntervalToInt32Seconds(&row.Duration),
		Views:           row.Views,
		Likes:           row.Likes,
		DefaultLang:     *row.DefaultLang,
		Embeddable:      row.Embeddable,
		ThumbnailURL:    *row.ThumbnailUrl,
		ThumbnailWidth:  *row.ThumbnailWidth,
		ThumbnailHeight: *row.ThumbnailHeight,
	}, nil
}

func (s *VideoService) updateVideo(ctx context.Context, id int64, video *Video) (*Video, error) {
	err := s.queries.UpdateVideo(ctx, dbgen.UpdateVideoParams{
		Title:           video.Title,
		ChannelTitle:    video.ChannelTitle,
		PublishedAt:     NewTimestamptz(video.PublishedAt),
		Duration:        NewInterval(int64(video.DurationSecs)),
		Views:           video.Views,
		Likes:           video.Likes,
		DefaultLang:     &video.DefaultLang,
		Embeddable:      video.Embeddable,
		ThumbnailUrl:    &video.ThumbnailURL,
		ThumbnailWidth:  &video.ThumbnailWidth,
		ThumbnailHeight: &video.ThumbnailHeight,
		ID:              id,
	})
	if err != nil {
		s.log.Error("failed to update video in DB",
			"id", id,
			"onlineVideoID", video.OnlineVideoID,
			"err", err,
		)
		return nil, err
	}
	s.log.Info("updated video in DB", "onlineVideoID", video.OnlineVideoID, "ID", id)
	video.ID = id
	return video, nil
}

/*
 * The function returns video metadata if it's available. Also, the metadata is stored to DB.
 */
func (s *VideoService) ProbeVideo(ctx context.Context, onlineVideoId string, force bool) (*Video, error) {
	existing, err := s.queries.GetVideo(ctx, onlineVideoId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to check video in DB", "onlineVideoId", onlineVideoId, "err", err)
			return nil, err
		}
	} else {
		if force {
			s.log.Info("force: reloading video details")
			video, err := s.ytClient.GetVideoInformation(ctx, onlineVideoId)
			if err != nil {
				return nil, err
			}
			updatedVideo, err := s.updateVideo(ctx, existing.ID, video)
			if err != nil {
				return nil, err
			}
			return updatedVideo, nil
		}

		s.log.Info("loaded video from DB", "onlineVideoId", onlineVideoId, "id", existing.ID)
		dbVideo, convErr := rowToVideo(&existing)
		if convErr != nil {
			s.log.Error("failed to convert loaded video", "onlineVideoId", onlineVideoId, "err", convErr)
			return nil, convErr
		}
		return dbVideo, nil
	}
	video, err := s.ytClient.GetVideoInformation(ctx, onlineVideoId)
	if err != nil {
		return nil, err
	}

	// XXX one of concurrent inserts will fail with a conflict (online_video_id is UNIQUE)
	videoID, err := s.queries.CreateVideo(ctx, dbgen.CreateVideoParams{
		OnlineVideoID:   video.OnlineVideoID,
		Title:           video.Title,
		ChannelID:       video.ChannelID,
		ChannelTitle:    video.ChannelTitle,
		PublishedAt:     NewTimestamptz(video.PublishedAt),
		Duration:        NewInterval(int64(video.DurationSecs)),
		Views:           video.Views,
		Likes:           video.Likes,
		DefaultLang:     &video.DefaultLang,
		Embeddable:      video.Embeddable,
		ThumbnailUrl:    &video.ThumbnailURL,
		ThumbnailWidth:  &video.ThumbnailWidth,
		ThumbnailHeight: &video.ThumbnailHeight,
	})
	if err != nil {
		s.log.Error("failed to store loaded to DB", "onlineVideoId", onlineVideoId, "err", err)
		return nil, err
	}
	s.log.Info("stored video to DB", "onlineVideoId", onlineVideoId, "ID", videoID)
	video.ID = videoID
	return video, nil
}

type VideoJob struct {
	jobID int64
	state dbgen.JobState
}

type VideoJobs struct {
	fetch VideoJob
	asr   VideoJob
}

type ProcessingState string

const (
	ProcessingStateNew          ProcessingState = "new"
	ProcessingStateAtCapacity   ProcessingState = "at_capacity"
	ProcessingStateFetchPending ProcessingState = "fetch_pending"
	ProcessingStateFetchRunning ProcessingState = "fetch_running"
	ProcessingStateAsrPending   ProcessingState = "asr_pending"
	ProcessingStateAsrRunning   ProcessingState = "asr_running"
	ProcessingStateDone         ProcessingState = "done"
	ProcessingStateFailed       ProcessingState = "failed"
)

type EnqueuedJob struct {
	VideoDurationSecs int32 `json:"video_duration_secs"`
}

type QuotaStatus struct {
	Day         string `json:"day"`
	UsedPercent int16  `json:"used_percent"`
}

type VideoProcess struct {
	State        ProcessingState `json:"state"`
	ErrorMessage string          `json:"error_message"`
	Queue        []EnqueuedJob   `json:"queue"`
	AsrQuota     QuotaStatus     `json:"asr_quota"`
}

type DashJob struct {
	JobType           dbgen.JobType  `json:"job_type"`
	JobState          dbgen.JobState `json:"job_state"`
	OnlineVideoID     string         `json:"online_video_id"`
	Title             string         `json:"title"`
	VideoDurationSecs int32          `json:"video_duration_secs"`
	CreatedAt         int64          `json:"created_at"`
}

type DashResponse struct {
	Last24hJobs []DashJob   `json:"last_24h_jobs"`
	AsrQuota    QuotaStatus `json:"asr_quota"`
}

type SuggestedVideo struct {
	OnlineVideoID   string `json:"online_video_id"`
	Title           string `json:"title"`
	ChannelTitle    string `json:"channel_title"`
	DurationSecs    int32  `json:"duration_secs"`
	ThumbnailURL    string `json:"thumbnail_url"`
	ThumbnailWidth  int32  `json:"thumbnail_width"`
	ThumbnailHeight int32  `json:"thumbnail_height"`
}

type SuggestedVideos struct {
	Videos []SuggestedVideo `json:"videos"`
}

/**
 * Just take an item with the largest job ID
 */
func getLatestJob(rows []dbgen.GetVideoJobsRow) *dbgen.GetVideoJobsRow {
	if len(rows) == 0 {
		return nil
	}
	result := &rows[0]
	for i := range rows {
		if rows[i].ID > result.ID {
			result = &rows[i]
		}
	}
	return result
}

func makeQueueForFetchJob(queue []dbgen.GetFetchJobQueueRow) []EnqueuedJob {
	result := make([]EnqueuedJob, 0, len(queue))
	for _, row := range queue {
		result = append(result, EnqueuedJob{pgIntervalToInt32Seconds(&row.Duration)})
	}
	return result
}

func makeQueueForAsrJob(queue []dbgen.GetAsrJobQueueRow) []EnqueuedJob {
	result := make([]EnqueuedJob, 0, len(queue))
	for _, row := range queue {
		result = append(result, EnqueuedJob{pgIntervalToInt32Seconds(&row.Duration)})
	}
	return result
}

func (s *VideoService) loadQuota(ctx context.Context) (QuotaStatus, error) {
	today := quota.GetTodayForQuota()
	usedSeconds, err := s.queries.GetAsrQuota(ctx, today)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return QuotaStatus{
				Day:         quota.PrintQuotaDate(today),
				UsedPercent: 0,
			}, nil
		}
		return QuotaStatus{}, err
	}
	return QuotaStatus{
		Day:         quota.PrintQuotaDate(today),
		UsedPercent: quota.CalculateUsedPercent(usedSeconds),
	}, nil
}

func (s *VideoService) GetVideoProcess(ctx context.Context, videoID int64) (VideoProcess, error) {
	rows, err := s.queries.GetVideoJobs(ctx, videoID)
	if err != nil {
		return VideoProcess{}, err
	}
	if len(rows) > 2 {
		s.log.Error("too many video jobs", "videoID", videoID, "jobs", len(rows))
		return VideoProcess{}, ErrCorruptedData
	}

	latestJob := getLatestJob(rows)

	if latestJob == nil {
		return VideoProcess{
			State: ProcessingStateNew,
		}, nil
	}

	// Below is the handling of the case when at least one job exists for the video

	if latestJob.State == dbgen.JobStateFailed {
		errorMessage := ""
		switch latestJob.Type {
		case dbgen.JobTypeFetch:
			errorMessage = "fetch fail"
		case dbgen.JobTypeAsr:
			lastErr := latestJob.LastError
			if lastErr != nil {
				errorMessage = fmt.Sprintf("asr fail: %s", *lastErr)
			} else {
				errorMessage = "asr fail"
			}
		}
		return VideoProcess{
			State:        ProcessingStateFailed,
			ErrorMessage: errorMessage,
		}, nil
	}

	// Below is the handling of the case when the video job hasn't failed.
	// More specifically: the job is either pending, running, or done.

	quota, err := s.loadQuota(ctx)
	if err != nil {
		s.log.Error("failed to load quota status", "err", err)
		return VideoProcess{}, err
	}

	if latestJob.Type == dbgen.JobTypeFetch {
		if latestJob.State == dbgen.JobStatePending {
			queue, err := s.queries.GetFetchJobQueue(ctx, latestJob.CreatedAt)
			if err != nil {
				s.log.Error("failed to load Fetch job queue", "created_before", latestJob.CreatedAt, "err", err)
				return VideoProcess{}, err
			}
			return VideoProcess{
				State:    ProcessingStateFetchPending,
				Queue:    makeQueueForFetchJob(queue),
				AsrQuota: quota,
			}, nil
		} else if latestJob.State == dbgen.JobStateRunning {
			createdBefore := NewTimestamptz(time.Now())
			// This Fetch job is already running.
			// It will need to wait for other ASR jobs once it graduates from Fetch to ASR.
			queue, err := s.queries.GetAsrJobQueue(ctx, createdBefore)
			if err != nil {
				s.log.Error("failed to load ASR job queue", "created_before", createdBefore, "err", err)
				return VideoProcess{}, err
			}
			return VideoProcess{
				State:    ProcessingStateFetchRunning,
				Queue:    makeQueueForAsrJob(queue),
				AsrQuota: quota,
			}, nil
		} else {
			// The only remaining option is 'done',
			// but if Fetch is done,
			// ASR job must be created under the same transaction
			// and take over as 'the latest'.
			s.log.Error("inconsistent Fetch job state", "job", latestJob.ID, "state", latestJob.State)
			return VideoProcess{}, ErrCorruptedData
		}
	} else if latestJob.Type == dbgen.JobTypeAsr {
		if latestJob.State == dbgen.JobStatePending {
			queue, err := s.queries.GetAsrJobQueue(ctx, latestJob.CreatedAt)
			if err != nil {
				s.log.Error("failed to load ASR job queue", "created_before", latestJob.CreatedAt, "err", err)
				return VideoProcess{}, err
			}
			return VideoProcess{
				State:    ProcessingStateAsrPending,
				Queue:    makeQueueForAsrJob(queue),
				AsrQuota: quota,
			}, nil
		} else if latestJob.State == dbgen.JobStateRunning {
			return VideoProcess{
				State: ProcessingStateAsrRunning,
			}, nil
		} else if latestJob.State == dbgen.JobStateDone {
			return VideoProcess{
				State: ProcessingStateDone,
			}, nil
		} else {
			s.log.Error("unsupported ASR job state", "job", latestJob.ID, "state", latestJob.State)
			return VideoProcess{}, ErrCorruptedData
		}
	} else {
		s.log.Error("unsupported job type", "job", latestJob.ID, "type", latestJob.Type)
		return VideoProcess{}, ErrCorruptedData
	}
}

func (s *VideoService) createVideoProcess(ctx context.Context, videoID int64) (VideoProcess, error) {
	byIDRow, err := s.queries.GetVideoByID(ctx, videoID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return VideoProcess{}, fmt.Errorf("video load fail: %w", err)
		}
		return VideoProcess{}, ErrNoSuchVideo
	}

	video, err := byIDRowToVideo(&byIDRow)
	if err != nil {
		s.log.Error("loaded video convert fail", "videoID", videoID, "err", err)
		return VideoProcess{}, err
	}

	obstacle := video.ProcessingObstacle()
	if obstacle != "" {
		s.log.Error("not allowed to process video", "videoID", videoID, "obstacle", obstacle)
		return VideoProcess{}, ErrUnprocessableVideo
	}

	enqueued, err := s.queries.GetFetchJobQueue(ctx, NewTimestamptz(time.Now()))
	enqueuedSize := len(enqueued)
	if enqueuedSize >= maxQueueSize {
		s.log.Error("max processing queue size reached", "size", enqueuedSize)
		return VideoProcess{
			State: ProcessingStateAtCapacity,
			Queue: makeQueueForFetchJob(enqueued),
		}, nil
	} else {
		s.log.Info("creating Fetch job", "queue", enqueuedSize, "video", videoID)
	}

	fetchJobID, err := s.queries.CreateFetchJobIfAbsent(ctx, dbgen.CreateFetchJobIfAbsentParams{
		VideoID:       videoID,
		OnlineVideoID: video.OnlineVideoID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to create fetch job", "videoID", videoID, "err", err)
			return VideoProcess{}, err
		}
		// If pgx.ErrNoRows, then probably a concurrent insert succeeded first.
		s.log.Warn("job insert returned nothing probably because conflict", "videoID", videoID)
		return s.GetVideoProcess(ctx, videoID)
	}
	s.log.Info("Fetch job created", "job", fetchJobID)
	return s.GetVideoProcess(ctx, videoID)
}

func (s *VideoService) GetOrCreateVideoProcess(ctx context.Context, videoID int64) (VideoProcess, error) {
	processVideo, err := s.GetVideoProcess(ctx, videoID)
	if err != nil {
		s.log.Error("failed to load existing jobs", "videoID", videoID, "err", err)
		return VideoProcess{}, err
	}

	if processVideo.State != ProcessingStateNew {
		return processVideo, nil
	}

	return s.createVideoProcess(ctx, videoID)
}

type TranscriptionInfo struct {
	ID    int64  `json:"id"`
	Model string `json:"model"`
}

func (s *VideoService) GetTranscriptions(ctx context.Context, videoID int64) ([]TranscriptionInfo, error) {
	transcriptions, err := s.queries.GetTranscriptionsByVideoID(ctx, videoID)
	if err != nil {
		s.log.Error("failed to load transcriptions from DB", "videoID", videoID, "err", err)
		return []TranscriptionInfo{}, err
	}
	result := make([]TranscriptionInfo, 0, len(transcriptions))
	for _, row := range transcriptions {
		result = append(result, TranscriptionInfo{
			ID:    row.ID,
			Model: row.Model,
		})
	}
	s.log.Info("loaded transcriptions for video", "videoID", videoID, "count", len(result))
	return result, nil
}

type SubtitleSpan struct {
	Items []subtitles.Subtitle `json:"items"`
	Next  int32                `json:"next"`
}

func (s *VideoService) FindSeqByStartMs(
	ctx context.Context,
	transcriptionID int64,
	startMs int32,
) (int32, error) {
	seq, err := s.queries.FindSeqByStartMs(ctx, dbgen.FindSeqByStartMsParams{
		TranscriptionID: transcriptionID,
		StartMs:         startMs,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoSuchSeq
	}
	return seq, err
}

func (s *VideoService) GetSubtitles(ctx context.Context, transcriptionID int64, startSeq int32, wordCount int32, minConfidence int16) (SubtitleSpan, error) {
	words, err := s.queries.GetWords(ctx, dbgen.GetWordsParams{
		TranscriptionID: transcriptionID,
		StartSeq:        startSeq,
		WordCount:       wordCount,
	})
	if err != nil {
		s.log.Error("failed to load words from DB", "transcription", transcriptionID, "err", err)
		return SubtitleSpan{}, err
	}
	if len(words) == 0 {
		_, err := s.queries.GetTranscription(ctx, transcriptionID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return SubtitleSpan{}, ErrNoSuchTranscription
			}
			return SubtitleSpan{}, err
		}
		return SubtitleSpan{
			Next: -1,
		}, nil
	}
	inputWords := make([]subtitles.InputWord, 0, len(words))
	maxSeq := int32(-1)
	for _, w := range words {
		var word string
		if w.Confidence >= minConfidence {
			word = w.Word
		} else {
			word = "[" + w.Word + "?]"
		}
		inputWords = append(inputWords, subtitles.InputWord{
			Word:    word,
			StartMs: int(w.StartMs),
			EndMs:   int(w.EndMs),
			Speaker: uint32(w.Speaker),
		})
		maxSeq = max(maxSeq, w.Seq)
	}
	subtitleItems := subtitles.Group(inputWords, 1000, 20)

	/**
	 * XXX pagination is simplified here.
	 * A better page start is after a subtitle end.
	 * But we split pages by word count: page 0 - words 0..99, page 1 - words 100..199, etc.
	 */
	var nextSeq int32
	if maxSeq >= 0 && len(words) >= int(wordCount) {
		nextSeq = maxSeq + 1
	} else {
		nextSeq = -1
	}

	return SubtitleSpan{
		Items: subtitleItems,
		Next:  nextSeq,
	}, nil
}

func (s *VideoService) GetDash(ctx context.Context) (DashResponse, error) {
	jobs, err := s.queries.GetLast24hJobs(ctx)
	if err != nil {
		return DashResponse{}, fmt.Errorf("failed to load last 24h jobs for /dash: %w", err)
	}
	dashJobs := make([]DashJob, 0, len(jobs))
	for _, job := range jobs {
		dashJobs = append(dashJobs, DashJob{
			JobType:           job.Type,
			JobState:          job.State,
			OnlineVideoID:     job.OnlineVideoID,
			Title:             job.Title,
			VideoDurationSecs: MicrosToFloorInt32Seconds(job.Duration.Microseconds),
			CreatedAt:         job.CreatedAt.Time.Unix(),
		})
	}

	asrQuota, err := s.loadQuota(ctx)
	if err != nil {
		return DashResponse{}, fmt.Errorf("failed to load quota status for /dash: %w", err)
	}

	return DashResponse{
		Last24hJobs: dashJobs,
		AsrQuota:    asrQuota,
	}, nil
}

func (s *VideoService) GetSuggestedVideos(ctx context.Context) (SuggestedVideos, error) {
	rows, err := s.queries.GetSuggestedVideos(ctx)
	if err != nil {
		return SuggestedVideos{}, fmt.Errorf("failed to load suggested videos: %w", err)
	}

	videos := make([]SuggestedVideo, 0, len(rows))
	for _, row := range rows {
		videos = append(videos, SuggestedVideo{
			OnlineVideoID:   row.OnlineVideoID,
			Title:           row.Title,
			ChannelTitle:    row.ChannelTitle,
			DurationSecs:    MicrosToFloorInt32Seconds(row.Duration.Microseconds),
			ThumbnailURL:    *row.ThumbnailUrl,
			ThumbnailWidth:  *row.ThumbnailWidth,
			ThumbnailHeight: *row.ThumbnailHeight,
		})
	}

	return SuggestedVideos{
		Videos: videos,
	}, nil
}
