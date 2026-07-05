package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/subtitles"
)

var (
	ErrCorruptedData       = errors.New("corrupted data")
	ErrUnprocessableVideo  = errors.New("unprocessable video")
	ErrNoSuchTranscription = errors.New("no transcription")
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

func (s *VideoService) CreateOrGetVideoJobs(ctx context.Context, videoID int64) (VideoJobs, error) {
	existingJobs, err := s.GetVideoJobs(ctx, videoID)
	if err != nil {
		s.log.Error("failed to load existing jobs", "videoID", videoID, "err", err)
		return VideoJobs{}, err
	}

	if existingJobs.GetProcessingState() != ProcessingStateNew {
		return existingJobs, nil
	}

	byIDRow, err := s.queries.GetVideoByID(ctx, videoID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return VideoJobs{}, fmt.Errorf("video load fail: %w", err)
		}
		return VideoJobs{}, ErrNoSuchVideo
	}

	video, err := byIDRowToVideo(&byIDRow)
	if err != nil {
		s.log.Error("loaded video convert fail", "videoID", videoID, "err", err)
		return VideoJobs{}, err
	}

	obstacle := video.ProcessingObstacle()
	if obstacle != "" {
		s.log.Error("not allowed to process video", "videoID", videoID, "obstacle", obstacle)
		return VideoJobs{}, ErrUnprocessableVideo
	}

	fetchJobID, err := s.queries.CreateFetchJobIfAbsent(ctx, dbgen.CreateFetchJobIfAbsentParams{
		VideoID:       videoID,
		OnlineVideoID: video.OnlineVideoID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to create fetch job", "videoID", videoID, "err", err)
			return VideoJobs{}, err
		}
		// If pgx.ErrNoRows, then probably a concurrent insert succeeded first.
		s.log.Warn("job insert returned nothing probably because conflict", "videoID", videoID)
		alreadyExisting, readErr := s.GetVideoJobs(ctx, videoID)
		if readErr != nil {
			s.log.Error("re-read after insert fail failed too", "videoID", videoID, "readErr", readErr)
			return VideoJobs{}, readErr
		}
		if alreadyExisting.GetProcessingState() == ProcessingStateNew {
			s.log.Error("re-read after insert fail returned no jobs", "videoID", videoID)
			return VideoJobs{}, err
		}
		s.log.Info("successful re-read after insert fail", "videoID", videoID, "fetch.jobID", alreadyExisting.fetch.jobID)
		return alreadyExisting, nil
	}

	return VideoJobs{
		fetch: VideoJob{
			jobID: fetchJobID,
			state: dbgen.JobStatePending,
		},
	}, nil
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
