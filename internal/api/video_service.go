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
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/llm"
	"github.com/mxwell/qarau/internal/pg"
	"github.com/mxwell/qarau/internal/quota"
	"github.com/mxwell/qarau/internal/subtitles"
)

var (
	ErrCorruptedData       = errors.New("corrupted data")
	ErrUnprocessableVideo  = errors.New("unprocessable video")
	ErrNoSuchTranscription = errors.New("no transcription")
	ErrNoSuchSeq           = errors.New("no seq")
	ErrNoSuchTopic         = errors.New("no such topic slug")
)

type VideoService struct {
	log      *slog.Logger
	queries  *dbgen.Queries
	pool     *pgxpool.Pool
	ytClient *YtClient
	llmQuota *quota.LlmQuotaController
}

func NewVideoService(
	log *slog.Logger,
	queries *dbgen.Queries,
	pool *pgxpool.Pool,
	ytClient *YtClient,
	llmQuota *quota.LlmQuotaController,
) (*VideoService, error) {
	if log == nil {
		return nil, errors.New("nil logger in VideoService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in VideoService creation")
	}
	if pool == nil {
		return nil, errors.New("nil pool in VideoService creation")
	}
	if ytClient == nil {
		return nil, errors.New("nil YT client in VideoService creation")
	}
	if llmQuota == nil {
		return nil, errors.New("nil LLM quota controller in VideoService creation")
	}
	return &VideoService{
		log:      log,
		queries:  queries,
		pool:     pool,
		ytClient: ytClient,
		llmQuota: llmQuota,
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
		PublishedAt:     pg.NewTimestamptz(video.PublishedAt),
		Duration:        pg.NewInterval(int64(video.DurationSecs)),
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
		PublishedAt:     pg.NewTimestamptz(video.PublishedAt),
		Duration:        pg.NewInterval(int64(video.DurationSecs)),
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
	LlmQuota    QuotaStatus `json:"llm_quota"`
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

type SuggestedPlaylists struct {
	Playlists  []Playlist `json:"playlists"`
	NextCursor *int64     `json:"next_cursor,omitempty"`
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

func (s *VideoService) loadLlmQuota(ctx context.Context) (QuotaStatus, error) {
	today := quota.GetTodayForQuota()
	row, err := s.queries.GetLlmQuota(ctx, today)
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
		Day: quota.PrintQuotaDate(today),
		UsedPercent: s.llmQuota.CalculateUsedPercent(
			quota.TokenUsage{
				Input:  row.UsedInputTokens,
				Output: row.UsedOutputTokens,
			},
		),
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
			createdBefore := pg.NewTimestamptz(time.Now())
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

	enqueued, err := s.queries.GetFetchJobQueue(ctx, pg.NewTimestamptz(time.Now()))
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
	subtitleItems := subtitles.Group(inputWords, 1000, 14)

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

// If start_ms is before any sentence, then return 0 (the first sentence)
func (s *VideoService) FindSentenceSeqByStartMs(
	ctx context.Context,
	transcriptionID int64,
	startMs int32,
) (int32, error) {
	seq, err := s.queries.FindSentenceSeqByStartMs(ctx, dbgen.FindSentenceSeqByStartMsParams{
		TranscriptionID: transcriptionID,
		StartMs:         startMs,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		firstSeq, err2 := s.queries.GetFirstSentenceSeq(ctx, transcriptionID)
		if errors.Is(err2, pgx.ErrNoRows) {
			return 0, ErrNoSuchSeq
		} else if err2 != nil {
			return seq, errors.Join(err, err2)
		}
		return firstSeq, nil
	}
	return seq, err
}

// Return availability, error
func (s *VideoService) checkLlmQuotaAvailable(ctx context.Context) (bool, error) {
	today := quota.GetTodayForQuota()
	llmQuotum, err := s.queries.GetLlmQuota(ctx, today)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			llmQuotum = dbgen.GetLlmQuotaRow{
				UsedInputTokens:  0,
				UsedOutputTokens: 0,
			}
		} else {
			return false, err
		}
	}
	quotaOk, quotaStatus := s.llmQuota.Available(
		quota.TokenUsage{
			Input:  llmQuotum.UsedInputTokens,
			Output: llmQuotum.UsedOutputTokens,
		},
	)
	if !quotaOk {
		s.log.Info("daily llm quota used up", "status", quotaStatus)
		return false, nil
	}
	return true, nil
}

func (s *VideoService) joinSentenceBreakdowns(
	sentences []dbgen.GetSentencesRangeRow,
	breakdowns []llm.BreakdownContent,
) []llm.SentBreakdown {
	i := 0
	n := len(sentences)
	result := make([]llm.SentBreakdown, 0)
	for j := range breakdowns {
		if i >= n {
			break
		}
		seq := breakdowns[j].Seq
		for i < n && sentences[i].Seq < seq {
			i++
		}
		if i < n && sentences[i].Seq == seq {
			result = append(result, llm.SentBreakdown{
				Seq:          seq,
				StartMs:      sentences[i].StartMs,
				EndMs:        sentences[i].EndMs,
				Text:         sentences[i].Text,
				Translations: breakdowns[j].Translations,
				Words:        breakdowns[j].Words,
			})
		}
	}
	if len(result) != 0 {
		s.log.Info("joined sentence and breakdowns", "sents", len(sentences), "breakdowns", len(breakdowns), "join", len(result))
	} else {
		s.log.Warn("empty sent & breakdown join result", "sents", len(sentences), "breakdowns", len(breakdowns))
	}
	return result
}

type GetBreakdownsResponse struct {
	Ok         bool                `json:"ok"`
	Breakdowns []llm.SentBreakdown `json:"breakdowns"`
	Message    string              `json:"message"`

	// batch boundaries
	StartMs    int32 `json:"start_ms"`
	EndMs      int32 `json:"end_ms"`
	BatchStart int32 `json:"batch_start"`

	// batch is running => client should keep polling
	BatchRunning bool `json:"batch_running"`

	// a sentence text (at `seq`) from the batch to show during generation
	Preview string `json:"preview"`
}

func (s *VideoService) GetBreakdowns(ctx context.Context, transcriptionID int64, targetLang string, sentSeq int32) (GetBreakdownsResponse, error) {
	batch, batchStart, err := llm.GetBatchBoundariesAndContent(
		ctx,
		s.queries,
		s.log,
		transcriptionID,
		sentSeq,
	)
	if err != nil {
		return GetBreakdownsResponse{}, err
	}
	if len(batch) == 0 {
		return GetBreakdownsResponse{
			Ok:      false,
			Message: "sentences not found",
		}, nil
	}

	// inclusive
	batchEnd := batchStart + int32(len(batch)) - 1
	batchStartMs := batch[0].StartMs
	batchEndMs := batch[len(batch)-1].EndMs
	s.log.Info(
		"loaded sent batch",
		"transcription", transcriptionID,
		"start", batchStart,
		"end", batchEnd,
		"start_ms", batchStartMs,
		"end_ms", batchEndMs,
	)
	dbBreakdowns, err := s.queries.GetSentenceBreakdowns(ctx, dbgen.GetSentenceBreakdownsParams{
		TranscriptionID: transcriptionID,
		StartSeq:        batchStart,
		EndSeq:          batchEnd,
		TargetLang:      targetLang,
	})
	if err != nil {
		s.log.Error(
			"breakdown batch load fail",
			"transcriptionID", transcriptionID,
			"batchStart", batchStart,
			"err", err,
		)
		return GetBreakdownsResponse{}, err
	}
	breakdowns := make([]llm.SentBreakdown, 0)
	if len(dbBreakdowns) > 0 {
		breakdownContent, err := llm.ToBreakdownContent(dbBreakdowns)
		if err != nil {
			return GetBreakdownsResponse{}, fmt.Errorf("existing breakdown load fail: %w", err)
		}
		breakdowns = s.joinSentenceBreakdowns(batch, breakdownContent)
	}

	// if all sentences are covered, return early, without batch check in DB
	if len(breakdowns) >= len(batch) {
		return GetBreakdownsResponse{
			Ok:         true,
			Breakdowns: breakdowns,
			Message:    "",
			StartMs:    batchStartMs,
			EndMs:      batchEndMs,
			BatchStart: batchStart,
		}, nil
	}

	preview := ""
	offset := sentSeq - batchStart
	if int(offset) < len(batch) {
		preview = batch[offset].Text
	}

	batchRow, err := s.queries.GetBreakdownBatch(ctx, dbgen.GetBreakdownBatchParams{
		TranscriptionID: transcriptionID,
		BatchStartSeq:   batchStart,
		TargetLang:      targetLang,
	})
	message := "breakdowns missing"
	batchRunning := false
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return GetBreakdownsResponse{}, err
		}
	} else {
		switch batchRow.State {
		case dbgen.BatchStatePending:
			message = "breakdowns pending"
		case dbgen.BatchStateRunning:
			message = "breakdowns running"
			batchRunning = batchRow.LockedUntil.Time.After(time.Now())
			s.log.Info(
				"batch state running",
				"transcriptionID", transcriptionID,
				"batchStart", batchStart,
				"lockedUntil", batchRow.LockedUntil,
				"batchRunning_flag", batchRunning,
			)
		case dbgen.BatchStateDone:
			message = "breakdowns done"
		case dbgen.BatchStateFailed:
			// leave the message "breakdowns missing" - POST will reset the batch
		}
	}

	return GetBreakdownsResponse{
		Ok:         len(breakdowns) > 0,
		Breakdowns: breakdowns,
		Message:    message,
		StartMs:    batchStartMs,
		EndMs:      batchEndMs,
		BatchStart: batchStart,

		BatchRunning: batchRunning,
		Preview:      preview,
	}, nil
}

type EnqueueBreakdownsResponse struct {
	ProceedToPolling bool   `json:"proceed_to_polling"`
	Message          string `json:"message"`
}

// Enqueue a new breakdown batch
//
// Steps:
// - get the batch boundaries
// - check if breakdowns exist in DB => return early if yes
// - check if batch exists in DB => return early if yes
// - check LLM quota => return early if no quota
// - create/update a batch in DB
//
// Return one of:
// - 'no sentences found' if sentence query returned an empty set
// - 'no quota' if LLM quota is used up for today
// - 'pending' or 'running' for corresponding batch state if it exists already
// - 'queue full'
// - empty = 'done' (no need to send results => delegate to GET /breakdowns)
func (s *VideoService) EnqueueBreakdowns(
	ctx context.Context,
	transcriptionID int64,
	targetLang string,
	sentSeq int32,
) (EnqueueBreakdownsResponse, error) {
	batch, batchStart, err := llm.GetBatchBoundariesAndContent(
		ctx,
		s.queries,
		s.log,
		transcriptionID,
		sentSeq,
	)
	if err != nil {
		return EnqueueBreakdownsResponse{}, err
	}
	if len(batch) == 0 {
		return EnqueueBreakdownsResponse{
			ProceedToPolling: false,
			Message:          "no sentences found",
		}, nil
	}

	// inclusive
	batchEnd := batchStart + int32(len(batch)) - 1
	batchStartMs := batch[0].StartMs
	batchEndMs := batch[len(batch)-1].EndMs
	s.log.Info(
		"loaded sent batch",
		"transcription", transcriptionID,
		"start", batchStart,
		"end", batchEnd,
		"start_ms", batchStartMs,
		"end_ms", batchEndMs,
	)

	breakdownCount, err := s.queries.CountSentenceBreakdowns(ctx, dbgen.CountSentenceBreakdownsParams{
		TranscriptionID: transcriptionID,
		StartSeq:        batchStart,
		EndSeq:          batchEnd,
		TargetLang:      targetLang,
	})
	if err != nil {
		return EnqueueBreakdownsResponse{}, err
	}
	if breakdownCount > 0 {
		s.log.Info(
			"sent breakdown exist",
			"transcription", transcriptionID,
			"lang", targetLang,
			"count", breakdownCount,
		)
		return EnqueueBreakdownsResponse{
			ProceedToPolling: true,
		}, nil
	}

	batchRow, err := s.queries.GetBreakdownBatch(ctx, dbgen.GetBreakdownBatchParams{
		TranscriptionID: transcriptionID,
		BatchStartSeq:   batchStart,
		TargetLang:      targetLang,
	})
	if err == nil {
		if batchRow.State != dbgen.BatchStateFailed {
			s.log.Info("non-failed batch exists", "batch", batchRow.ID, "state", batchRow.State)
			return EnqueueBreakdownsResponse{
				ProceedToPolling: true,
			}, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return EnqueueBreakdownsResponse{}, err
	}

	// Two options at this point:
	// - no batch exists in DB
	// - failed batch in DB

	// check quota before batch enqueueing
	quotaOk, err := s.checkLlmQuotaAvailable(ctx)
	if err != nil {
		return EnqueueBreakdownsResponse{}, err
	}
	if !quotaOk {
		return EnqueueBreakdownsResponse{
			ProceedToPolling: false,
			Message:          "no quota",
		}, nil
	}

	active, err := s.queries.CountActiveBreakdownBatches(ctx, transcriptionID)
	if err != nil {
		return EnqueueBreakdownsResponse{}, err
	}
	if active >= 3 {
		s.log.Info("too many active batches", "transcription", transcriptionID, "active", active)
		return EnqueueBreakdownsResponse{
			ProceedToPolling: false,
			Message:          "queue full",
		}, nil
	}

	enqueued, err := s.queries.EnqueueBreakdownBatch(ctx, dbgen.EnqueueBreakdownBatchParams{
		TranscriptionID: transcriptionID,
		BatchStartSeq:   batchStart,
		TargetLang:      targetLang,
	})
	if err != nil {
		s.log.Error("failed to enqueue breakdown batch", "transcription", transcriptionID, "err", err)
		return EnqueueBreakdownsResponse{}, err
	}
	s.log.Info("enqueued breakdown batch", "transcription", transcriptionID, "state", enqueued.State)
	return EnqueueBreakdownsResponse{
		ProceedToPolling: true,
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

	llmQuota, err := s.loadLlmQuota(ctx)
	if err != nil {
		return DashResponse{}, fmt.Errorf("failed to load llm quota: %w", err)
	}

	return DashResponse{
		Last24hJobs: dashJobs,
		AsrQuota:    asrQuota,
		LlmQuota:    llmQuota,
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

type TopicsResponse struct {
	Slugs []string `json:"slugs"`
}

func (s *VideoService) GetTopics(ctx context.Context) (TopicsResponse, error) {
	slugs, err := s.queries.GetSortedTopicSlugs(ctx)
	if err != nil {
		return TopicsResponse{}, fmt.Errorf("topic slugs load fail: %w", err)
	}
	if len(slugs) == 0 {
		return TopicsResponse{}, errors.New("no topic slugs found")
	}
	s.log.Info("loaded topics", "topics", len(slugs))
	return TopicsResponse{
		Slugs: slugs,
	}, nil
}

type VideosOnTopicsResponse struct {
	AvailableTopics []string         `json:"available_topics"`
	Videos          []SuggestedVideo `json:"videos"`
}

func recommendRowToSuggestedVideo(row dbgen.RecommendVideosByTopicsRow) (*SuggestedVideo, error) {
	if row.ThumbnailUrl == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailWidth == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailHeight == nil {
		return nil, ErrCorruptedData
	}
	return &SuggestedVideo{
		OnlineVideoID:   row.OnlineVideoID,
		Title:           row.Title,
		ChannelTitle:    row.ChannelTitle,
		DurationSecs:    MicrosToFloorInt32Seconds(row.Duration.Microseconds),
		ThumbnailURL:    *row.ThumbnailUrl,
		ThumbnailWidth:  *row.ThumbnailWidth,
		ThumbnailHeight: *row.ThumbnailHeight,
	}, nil
}

func (s *VideoService) GetVideosOnTopics(ctx context.Context, requestSlugs []string, limit int) (VideosOnTopicsResponse, error) {
	availableSlugs, err := s.queries.GetSortedTopicSlugs(ctx)
	if err != nil {
		return VideosOnTopicsResponse{}, fmt.Errorf("topic slugs load fail: %w", err)
	}
	availableMap := make(map[string]bool, len(availableSlugs))
	for _, slug := range availableSlugs {
		availableMap[slug] = true
	}
	for _, slug := range requestSlugs {
		if !availableMap[slug] {
			s.log.Info(
				"unknown topic slug in recommend request",
				"slugs", len(requestSlugs),
				"slug", slug,
			)
			return VideosOnTopicsResponse{}, ErrNoSuchTopic
		}
	}
	videoRows, err := s.queries.RecommendVideosByTopics(ctx, dbgen.RecommendVideosByTopicsParams{
		Slugs:    requestSlugs,
		PageSize: int32(limit),
	})
	if err != nil {
		return VideosOnTopicsResponse{}, fmt.Errorf("videos by topics load fail: %w", err)
	}
	videos := make([]SuggestedVideo, 0, len(videoRows))
	for _, row := range videoRows {
		converted, err := recommendRowToSuggestedVideo(row)
		if err != nil {
			return VideosOnTopicsResponse{}, fmt.Errorf("recommended video conversion fail: %w", err)
		}
		videos = append(videos, *converted)
	}
	s.log.Info(
		"loaded video recommendations",
		"slugs", len(requestSlugs),
		"available", len(availableSlugs),
		"videos", len(videos),
	)
	return VideosOnTopicsResponse{
		AvailableTopics: availableSlugs,
		Videos:          videos,
	}, nil
}

func (s *VideoService) GetSuggestedPlaylists(ctx context.Context, cursor *int64, pageSize int32) (SuggestedPlaylists, error) {
	rowsWithExtra, err := s.queries.ListPlaylists(ctx, dbgen.ListPlaylistsParams{
		Cursor:   cursor,
		PageSize: pageSize + 1,
	})
	if err != nil {
		return SuggestedPlaylists{}, fmt.Errorf("failed to load suggested playlists from DB: %w", err)
	}
	rows := rowsWithExtra
	var nextCursor *int64
	if len(rowsWithExtra) > int(pageSize) {
		rows = rowsWithExtra[:pageSize]
		lastID := rows[len(rows)-1].ID
		nextCursor = &lastID
	}

	toLoadFromApi := make([]string, 0)
	idsToLoadFromApi := make([]int64, 0)
	for _, row := range rows {
		if row.Title != "" {
			continue
		}
		toLoadFromApi = append(toLoadFromApi, row.OnlinePlaylistID)
		idsToLoadFromApi = append(idsToLoadFromApi, row.ID)
	}

	loads := make(map[string]PlaylistLoad)

	if len(toLoadFromApi) > 0 {
		s.log.Info("loading playlist details from API", "playlists", len(toLoadFromApi))
		loads, err = s.ytClient.LoadPlaylists(ctx, toLoadFromApi)
		if err != nil {
			return SuggestedPlaylists{}, fmt.Errorf("failed to load suggested playlist details from API: %w", err)
		}
		updatedDetails := 0
		updatedWithError := 0
		for i, onlinePlaylistID := range toLoadFromApi {
			id := idsToLoadFromApi[i]
			load := loads[onlinePlaylistID]
			playlist := load.playlist
			if playlist != nil {
				err = s.queries.UpdatePlaylistDetails(ctx, dbgen.UpdatePlaylistDetailsParams{
					Title:           playlist.Title,
					ItemCount:       playlist.ItemCount,
					ThumbnailUrl:    playlist.ThumbnailURL,
					ThumbnailWidth:  playlist.ThumbnailWidth,
					ThumbnailHeight: playlist.ThumbnailHeight,
					ID:              id,
				})
				if err != nil {
					return SuggestedPlaylists{}, fmt.Errorf(
						"failed to update playlist details in DB: %w",
						err,
					)
				}
				updatedDetails += 1
			} else {
				errMessage := load.errMsg
				if errMessage == "" {
					errMessage = "unknown error"
				}
				err = s.queries.UpdatePlaylistWithError(ctx, dbgen.UpdatePlaylistWithErrorParams{
					Error: &errMessage,
					ID:    id,
				})
				if err != nil {
					return SuggestedPlaylists{}, fmt.Errorf(
						"failed to update playlist with error '%s' in DB: %w",
						errMessage,
						err,
					)
				}
				updatedWithError += 1
			}
		}
		s.log.Info("updated playlists in DB", "updatedDetails", updatedDetails, "updatedWithError", updatedWithError)
	}

	results := make([]Playlist, 0, len(rows))
	for _, row := range rows {
		onlinePlaylistID := row.OnlinePlaylistID
		if row.Title != "" {
			results = append(results, Playlist{
				OnlinePlaylistID: onlinePlaylistID,
				Title:            row.Title,
				ThumbnailURL:     row.ThumbnailUrl,
				ThumbnailWidth:   row.ThumbnailWidth,
				ThumbnailHeight:  row.ThumbnailHeight,
				ItemCount:        row.ItemCount,
			})
			continue
		}
		load := loads[onlinePlaylistID]
		playlist := load.playlist
		if playlist != nil {
			results = append(results, *playlist)
		} else {
			s.log.Warn(
				"skipping playlist with error from API",
				"onlinePlaylistID", onlinePlaylistID,
				"error", load.errMsg,
			)
			continue
		}
	}

	return SuggestedPlaylists{
		Playlists:  results,
		NextCursor: nextCursor,
	}, nil
}

func (s *VideoService) PlaylistPage(
	ctx context.Context,
	onlinePlaylistID, pageToken string,
) (PlaylistPage, error) {
	page, err := s.ytClient.LoadPlaylistPage(ctx, onlinePlaylistID, pageToken)
	if err != nil {
		s.log.Error(
			"failed to load playlist page",
			"list", onlinePlaylistID,
			"pageToken", pageToken,
			"err", err,
		)
		return PlaylistPage{}, fmt.Errorf("failed to load playlist page: %w", err)
	}
	return page, nil
}
