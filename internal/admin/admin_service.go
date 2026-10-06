package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/constants"
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

type EvalSetBatch struct {
	VideoID       int64    `json:"video_id"`
	OnlineVideoID string   `json:"online_video_id"`
	VideoTitle    string   `json:"video_title"`
	ChannelTitle  string   `json:"channel_title"`
	StartSentSeq  int32    `json:"start_sent_seq"`
	Sentences     []string `json:"sentences"`
}

type GenerateEvalSetResponse struct {
	Batches []EvalSetBatch `json:"batches"`
}

func (s *AdminService) GenerateEvalSet(ctx context.Context) (GenerateEvalSetResponse, error) {
	const (
		randomSentLimit = 100
		evalSetBatches  = 5
	)

	randomSentKeys, err := s.queries.GetRandomSentenceKeys(ctx, dbgen.GetRandomSentenceKeysParams{
		Batch: constants.SentenceBatchSize,
		Limit: randomSentLimit,
	})
	if err != nil {
		s.log.Error(
			"random sent keys fail",
			"limit", randomSentLimit,
			"batch", constants.SentenceBatchSize,
			"err", err,
		)
		return GenerateEvalSetResponse{}, err
	}
	usedTranscriptionIds := make(map[int64]bool)

	sampleKeys := make([]dbgen.GetRandomSentenceKeysRow, 0, evalSetBatches)
	for _, row := range randomSentKeys {
		if len(sampleKeys) >= evalSetBatches {
			break
		}
		if _, used := usedTranscriptionIds[row.TranscriptionID]; used {
			continue
		}
		usedTranscriptionIds[row.TranscriptionID] = true
		sampleKeys = append(sampleKeys, dbgen.GetRandomSentenceKeysRow{
			TranscriptionID: row.TranscriptionID,
			Seq:             row.Seq - constants.SentenceBatchSize,
		})
	}

	if len(sampleKeys) < evalSetBatches {
		s.log.Error(
			"failed to sample enough data for eval set",
			"keys", len(sampleKeys),
			"unfiltered", len(randomSentKeys),
		)
		return GenerateEvalSetResponse{}, errors.New("not enough data")
	}

	batches := make([]EvalSetBatch, 0, len(sampleKeys))
	for _, row := range sampleKeys {
		transcriptionID := row.TranscriptionID
		sents, err := s.queries.GetSentencesRange(ctx, dbgen.GetSentencesRangeParams{
			TranscriptionID: transcriptionID,
			StartSeq:        row.Seq,
			EndSeq:          row.Seq + constants.SentenceBatchSize,
		})
		if err != nil {
			s.log.Error("failed to load sentences for eval set", "err", err)
			return GenerateEvalSetResponse{}, fmt.Errorf("failed to load sentences for eval set: %w", err)
		}
		sentStrings := make([]string, len(sents))
		for i := range sents {
			sentStrings[i] = sents[i].Text
		}
		video, err := s.queries.GetVideoByTranscriptionID(ctx, transcriptionID)
		if err != nil {
			s.log.Error(
				"failed to load video details for eval set",
				"transcription", transcriptionID,
				"err", err,
			)
			return GenerateEvalSetResponse{}, fmt.Errorf("failed to load video details: %w", err)
		}
		batches = append(batches, EvalSetBatch{
			VideoID:       video.ID,
			OnlineVideoID: video.OnlineVideoID,
			VideoTitle:    video.Title,
			ChannelTitle:  video.ChannelTitle,
			StartSentSeq:  row.Seq,
			Sentences:     sentStrings,
		})
	}

	return GenerateEvalSetResponse{
		Batches: batches,
	}, nil
}
