package api

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/mxwell/qarau/db/gen"
)

var (
	ErrCorruptedData = errors.New("corrupted data")
	ErrNoVideoJobs   = errors.New("no jobs for video")
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

const microsecondsPerSecond = 1_000_000

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
		DurationSecs:    int(row.Duration.Microseconds / microsecondsPerSecond),
		DefaultLang:     *row.DefaultLang,
		Embeddable:      row.Embeddable,
		ThumbnailURL:    *row.ThumbnailUrl,
		ThumbnailWidth:  *row.ThumbnailWidth,
		ThumbnailHeight: *row.ThumbnailHeight,
	}, nil
}

/*
 * The function returns video metadata if it's available. Also, the metadata is stored to DB.
 */
func (s *VideoService) ProbeVideo(ctx context.Context, onlineVideoId string) (*Video, error) {
	existing, err := s.queries.GetVideo(ctx, onlineVideoId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to check video in DB", "onlineVideoId", onlineVideoId, "err", err)
			return nil, err
		}
	} else {
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
	s.log.Info("loaded video information from external API", "onlineVideoId", onlineVideoId, "title", video.Title, "lang", video.DefaultLang)

	duration := pgtype.Interval{
		Microseconds: int64(video.DurationSecs) * microsecondsPerSecond,
		Valid:        true,
	}

	// XXX one of concurrent inserts will fail with a conflict (online_video_id is UNIQUE)
	videoID, err := s.queries.CreateVideo(ctx, dbgen.CreateVideoParams{
		OnlineVideoID:   video.OnlineVideoID,
		Title:           video.Title,
		ChannelID:       video.ChannelID,
		ChannelTitle:    video.ChannelTitle,
		PublishedAt:     pgtype.Timestamptz{Time: video.PublishedAt, Valid: true},
		Duration:        duration,
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
	ProcessingStateNew     ProcessingState = "new"
	ProcessingStatePending ProcessingState = "pending"
	ProcessingStateRunning ProcessingState = "running"
	ProcessingStateDone    ProcessingState = "done"
	ProcessingStateFailed  ProcessingState = "failed"
)

func (jobs VideoJobs) GetProcessingState() ProcessingState {
	if jobs.fetch.jobID == 0 {
		return ProcessingStateNew
	}
	switch jobs.fetch.state {
	case dbgen.JobStatePending:
		return ProcessingStatePending
	case dbgen.JobStateRunning:
		return ProcessingStateRunning
	case dbgen.JobStateDone:
		if jobs.asr.jobID == 0 {
			return ProcessingStateFailed
		}
		switch jobs.asr.state {
		case dbgen.JobStateDone:
			return ProcessingStateDone
		case dbgen.JobStateFailed:
			return ProcessingStateFailed
		default:
			return ProcessingStateRunning
		}
	default:
		return ProcessingStateFailed
	}
}

func (s *VideoService) GetVideoJobs(ctx context.Context, videoID int64) (VideoJobs, error) {
	rows, err := s.queries.GetVideoJobs(ctx, videoID)
	if err != nil {
		return VideoJobs{}, err
	}
	if len(rows) > 2 {
		s.log.Error("too many video jobs", "videoID", videoID, "jobs", len(rows))
		return VideoJobs{}, ErrCorruptedData
	}
	var fetchJob VideoJob
	var asrJob VideoJob
	for _, row := range rows {
		switch row.Type {
		case dbgen.JobTypeFetch:
			fetchJob = VideoJob{
				jobID: row.ID,
				state: row.State,
			}
		case dbgen.JobTypeAsr:
			asrJob = VideoJob{
				jobID: row.ID,
				state: row.State,
			}
		default:
			s.log.Error("unknown job type", "videoID", videoID, "jobID", row.ID, "type", row.Type)
			return VideoJobs{}, ErrCorruptedData
		}
	}
	s.log.Info("loaded video jobs", "videoID", videoID, "fetch", fetchJob.jobID, "fetch_state", fetchJob.state, "asr", asrJob.jobID, "asr_state", asrJob.state)
	return VideoJobs{
		fetch: fetchJob,
		asr:   asrJob,
	}, nil
}

func (s *VideoService) CreateFetchJob(ctx context.Context, videoID int64) (jobID int64, err error) {
	// TODO
	return 0, nil
}
