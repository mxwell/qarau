package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/recommend"
)

type AdminService struct {
	log     *slog.Logger
	queries *dbgen.Queries
	pool    *pgxpool.Pool
}

func NewAdminService(
	log *slog.Logger,
	queries *dbgen.Queries,
	pool *pgxpool.Pool,
) (*AdminService, error) {
	if log == nil {
		return nil, errors.New("nil logger in AdminService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in AdminService creation")
	}
	if pool == nil {
		return nil, errors.New("nil pool in AdminService creation")
	}
	return &AdminService{
		log:     log,
		queries: queries,
		pool:    pool,
	}, nil
}

type JobListItem struct {
	JobType           dbgen.JobType  `json:"job_type"`
	JobState          dbgen.JobState `json:"job_state"`
	OnlineVideoID     string         `json:"online_video_id"`
	Title             string         `json:"title"`
	VideoDurationSecs int32          `json:"video_duration_secs"`
	CreatedAt         int64          `json:"created_at"`
}

func (s *AdminService) GetJobList(ctx context.Context) ([]JobListItem, error) {
	jobs, err := s.queries.GetLastJobs(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("failed to load last 24h jobs: %w", err)
	}
	result := make([]JobListItem, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, JobListItem{
			JobType:           job.Type,
			JobState:          job.State,
			OnlineVideoID:     job.OnlineVideoID,
			Title:             job.Title,
			VideoDurationSecs: api.MicrosToFloorInt32Seconds(job.Duration.Microseconds),
			CreatedAt:         job.CreatedAt.Time.Unix(),
		})
	}
	return result, nil
}

// What the function does:
// * checks topic slugs and matches with topic IDs
// * finds video ID by `onlineVideoID`
// * checks video ID refers to a video with transcriptions
// * removes existing topics for the video
// * inserts new video_topics rows
func (s *AdminService) SetVideoTopics(ctx context.Context, onlineVideoID string, topics []string) error {
	activeTopics, err := s.queries.GetActiveTopics(ctx)
	if err != nil {
		s.log.Error("failed to load topics", "err", err)
		return fmt.Errorf("topic load fail: %w", err)
	}
	available := make(map[string]int16)
	for _, row := range activeTopics {
		available[row.Slug] = row.ID
	}
	topicRows, err := recommend.SlugsToTopics(available, topics)
	if err != nil {
		s.log.Error("failed to convert topics", "err", err)
		return fmt.Errorf("topic conversion fail: %w", err)
	}

	videoID, err := s.queries.GetVideoID(ctx, onlineVideoID)
	if err != nil {
		s.log.Error("failed to find video ID", "onlineVideoID", onlineVideoID, "err", err)
		return fmt.Errorf("failed to find video ID: %w", err)
	}

	transcriptions, err := s.queries.CountTranscriptionsByVideoID(ctx, videoID)
	if err != nil {
		s.log.Error("failed to count transcriptions", "video", videoID, "err", err)
		return fmt.Errorf("transcription check fail: %w", err)
	}
	if transcriptions == 0 {
		s.log.Info("video without transcriptions in SetVideoTopics", "video", videoID)
		return errors.New("video without transcriptions")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe even after tx.Commit() -- does nothing

	qtx := s.queries.WithTx(tx)

	err = qtx.ResetVideoTopics(ctx, videoID)
	if err != nil {
		s.log.Error("failed to reset video topics", "video", videoID, "err", err)
		return fmt.Errorf("video topics reset fail: %w", err)
	}

	for i, topicRow := range topicRows {
		err = qtx.SetVideoTopic(ctx, dbgen.SetVideoTopicParams{
			VideoID: videoID,
			TopicID: topicRow.ID,
		})
		if err != nil {
			s.log.Error(
				"failed to set video topic",
				"video", videoID,
				"i", i,
				"topics", len(topicRows),
				"err", err,
			)
			return fmt.Errorf("video topic setting fail: %w", err)
		}
	}

	if len(topicRows) > 0 {
		s.log.Info("inserted topics for video", "video", videoID, "topics", len(topicRows))
	} else {
		s.log.Info("set no topics for video", "video", videoID)
	}

	return tx.Commit(ctx)
}
