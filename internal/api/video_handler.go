package api

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/mxwell/qarau/internal/subtitles"
)

var (
	onlineVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	errInvalidParam      = errors.New("invalid param")
)

type VideoHandler struct {
	log *slog.Logger
	svc *VideoService
}

func NewVideoHandler(log *slog.Logger, svc *VideoService) (*VideoHandler, error) {
	if log == nil {
		return nil, errors.New("nil logger in VideoHandler creation")
	}
	if svc == nil {
		return nil, errors.New("nil svc in VideoHandler creation")
	}
	return &VideoHandler{
		log: log,
		svc: svc,
	}, nil
}

func (h *VideoHandler) Register(r fiber.Router) {
	r.Get("/suggested_videos", h.SuggestedVideos)
	r.Get("/suggested_playlists", h.SuggestedPlaylists)
	r.Get("/probe/:online_video_id", h.Probe)
	r.Post("/fetch/:video_id", h.Fetch)
	r.Get("/subtitles/:transcription_id", h.Subtitles)
	r.Get("/export/:transcription_id", h.Export)

	r.Get("/dash", h.Dash)
}

type ProbedVideoResponse struct {
	ID                 int64               `json:"id"`
	Info               APIInfo             `json:"info"`
	ProcessingObstacle string              `json:"processing_obstacle"`
	Process            VideoProcess        `json:"process"`
	Transcriptions     []TranscriptionInfo `json:"transcriptions"`
}

func (h *VideoHandler) Probe(c *fiber.Ctx) error {
	onlineVideoID := c.Params("online_video_id")
	if !onlineVideoIDPattern.MatchString(onlineVideoID) {
		h.log.Info("invalid argument", "onlineVideoID", onlineVideoID)
		return badRequest(c, "invalid online_video_id")
	}
	force := c.Query("force", "0") == "1"
	video, err := h.svc.ProbeVideo(c.UserContext(), onlineVideoID, force)
	if err != nil {
		h.log.Info("video probe failed", "onlineVideoID", onlineVideoID, "err", err)
		if errors.Is(err, ErrNoSuchVideo) {
			return notFound(c, err.Error())
		}
		return internalError(c, "internal error while probing video")
	}

	videoProcess := VideoProcess{
		State: ProcessingStateNew,
	}
	transcriptions := make([]TranscriptionInfo, 0)
	if video.LoadedFromDB {
		videoProcess, err = h.svc.GetVideoProcess(c.UserContext(), video.ID)
		if err != nil {
			h.log.Error("failed to load process video state", "onlineVideoID", onlineVideoID, "err", err)
			return internalError(c, "internal error while checking processing state")
		}

		transcriptions, err = h.svc.GetTranscriptions(c.UserContext(), video.ID)
		if err != nil {
			return internalError(c, "internal error while loading transcriptions")
		}
		if videoProcess.State == ProcessingStateDone && len(transcriptions) == 0 {
			h.log.Error("no transcriptions for ProcessingStateDone", "videoID", video.ID)
			return internalError(c, "corrupted data")
		}
	}

	h.log.Info("checked video process state", "onlineVideoID", onlineVideoID, "state", videoProcess.State)
	apiInfo := NewAPIInfo(video)
	response := ProbedVideoResponse{
		ID:                 video.ID,
		Info:               apiInfo,
		ProcessingObstacle: video.ProcessingObstacle(),
		Process:            videoProcess,
		Transcriptions:     transcriptions,
	}
	return c.JSON(response)
}

func (h *VideoHandler) Fetch(c *fiber.Ctx) error {
	videoIDParam := c.Params("video_id")
	videoID, err := strconv.ParseInt(videoIDParam, 10, 64)
	if err != nil || videoID <= 0 {
		h.log.Info("invalid argument", "videoID", videoIDParam)
		return badRequest(c, "invalid video_id")
	}
	videoProcess, err := h.svc.GetOrCreateVideoProcess(c.UserContext(), videoID)
	if err != nil {
		if errors.Is(err, ErrNoSuchVideo) {
			h.log.Info("no such video", "videoID", videoID)
			return notFound(c, err.Error())
		}
		if errors.Is(err, ErrUnprocessableVideo) {
			return unprocessable(c, err.Error())
		}
		h.log.Error("failed to create fetch job", "videoID", videoID, "err", err)
		return internalError(c, "internal error")
	}
	return c.JSON(videoProcess)
}

func (h *VideoHandler) getTranscriptionIDParam(c *fiber.Ctx) (int64, error) {
	transcriptionIDString := c.Params("transcription_id")
	transcriptionID, err := strconv.ParseInt(transcriptionIDString, 10, 64)
	if err != nil {
		h.log.Info("failed to parse transcriptionID", "param", transcriptionIDString, "err", err)
		return 0, err
	}
	if transcriptionID < 0 {
		h.log.Info("negative transcriptionID", "transcriptionID", transcriptionID)
		return 0, errInvalidParam
	}
	return transcriptionID, nil
}

func (h *VideoHandler) Subtitles(c *fiber.Ctx) error {
	transcriptionID, err := h.getTranscriptionIDParam(c)
	if err != nil {
		return badRequest(c, "invalid transcription_id")
	}

	seq := int32(0)

	if startMsStr := c.Query("start_ms"); startMsStr != "" {
		if seqStr := c.Query("seq"); seqStr != "" {
			h.log.Info("both start_ms and seq set in params", "start_ms", startMsStr, "seq", seqStr)
			return badRequest(c, "provide either seq or start_ms, not both")
		}
		startMs, err := strconv.ParseInt(startMsStr, 10, 64)
		if err != nil {
			h.log.Info("failed to parse start_ms", "param", startMsStr, "err", err)
			return badRequest(c, "invalid start_ms")
		}
		if startMs < 0 || startMs > math.MaxInt32 {
			h.log.Info("invalid start_ms", "start_ms", startMs)
			return badRequest(c, "invalid start_ms")
		}
		seq, err = h.svc.FindSeqByStartMs(c.UserContext(), transcriptionID, int32(startMs))
		if err != nil {
			if errors.Is(err, ErrNoSuchSeq) {
				// No seq found => set seq to max and
				// let GetSubtitles() tell apart 'no transcription' Vs 'no more words'
				seq = seqBeyondEnd
			} else {
				h.log.Error("seq search by start_ms failed", "transcriptionID", transcriptionID, "start_ms", startMs, "err", err)
				return internalError(c, "internal error")
			}
		}
	} else if seqStr := c.Query("seq"); seqStr != "" {
		seqValue64, err := strconv.ParseInt(seqStr, 10, 64)
		if err != nil {
			h.log.Info("failed to parse seq", "param", seqStr, "err", err)
			return badRequest(c, "invalid seq")
		}
		if seqValue64 < 0 || seqValue64 > seqBeyondEnd {
			h.log.Info("invalid seq", "seq", seqValue64)
			return badRequest(c, "invalid seq")
		}
		seq = int32(seqValue64)
	}

	wordCount := c.QueryInt("word_count", 100)
	if wordCount <= 0 || wordCount > 1000 {
		h.log.Info("invalid word_count", "word_count", wordCount)
		return badRequest(c, "invalid word_count")
	}
	confidence := c.QueryInt("confidence", 0)
	if confidence < 0 || confidence > 100 {
		h.log.Info("invalid subtitles confidence", "confidence", confidence)
		return badRequest(c, "invalid confidence")
	}

	subtitleSpan, err := h.svc.GetSubtitles(
		c.UserContext(),
		transcriptionID,
		seq,
		int32(wordCount),
		int16(confidence),
	)
	if err != nil {
		if errors.Is(err, ErrNoSuchTranscription) {
			h.log.Info("no such transcription", "transcriptionID", transcriptionID, "err", err)
			return notFound(c, "transcription not found")
		}
		h.log.Error("failed to get subtitles", "transcriptionID", transcriptionID, "err", err)
		return internalError(c, "internal error")
	}
	return c.JSON(subtitleSpan)
}

func (h *VideoHandler) Export(c *fiber.Ctx) error {
	transcriptionID, err := h.getTranscriptionIDParam(c)
	if err != nil {
		return badRequest(c, "invalid transcription_id")
	}

	subtitleSpan, err := h.svc.GetSubtitles(
		c.UserContext(),
		transcriptionID,
		/* seq */ 0,
		/* wordCount */ math.MaxInt32, /* effectively unbounded */
		/* minConfidence */ 0,
	)
	if err != nil {
		if errors.Is(err, ErrNoSuchTranscription) {
			h.log.Info("no such transcription", "transcriptionID", transcriptionID, "err", err)
			return notFound(c, "transcription not found")
		}
		h.log.Error("failed to get subtitles", "transcriptionID", transcriptionID, "err", err)
		return internalError(c, "internal error")
	}

	result := subtitles.ConvertFrom(subtitleSpan.Items)
	h.log.Info(
		"rendered SRT response",
		"transcriptionID", transcriptionID,
		"size", len(result),
		"subtitles", len(subtitleSpan.Items),
	)

	c.Set(fiber.HeaderContentType, "application/x-subrip; charset=utf-8")
	c.Attachment(fmt.Sprintf("transcription_%d.srt", transcriptionID))
	return c.SendString(result)
}

func (h *VideoHandler) Dash(c *fiber.Ctx) error {
	response, err := h.svc.GetDash(c.UserContext())
	if err != nil {
		h.log.Error("failed to get data for /dash", "err", err)
		return internalError(c, "internal error")
	}
	return c.JSON(response)
}

func (h *VideoHandler) SuggestedVideos(c *fiber.Ctx) error {
	response, err := h.svc.GetSuggestedVideos(c.UserContext())
	if err != nil {
		h.log.Error("failed to load suggested videos", "err", err)
		return internalError(c, "internal error")
	}
	return c.JSON(response)
}

func (h *VideoHandler) SuggestedPlaylists(c *fiber.Ctx) error {
	var cursor *int64
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		value, err := strconv.ParseInt(cursorStr, 10, 64)
		if err != nil {
			h.log.Info("invalid cursor for suggested playlists", "err", err)
			return badRequest(c, "invalid cursor")
		}
		cursor = &value
	}
	response, err := h.svc.GetSuggestedPlaylists(c.UserContext(), cursor, 10)
	if err != nil {
		h.log.Error("failed to load suggested playlists", "err", err)
		return internalError(c, "internal error")
	}
	return c.JSON(response)
}

func badRequest(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusBadRequest, message)
}

func notFound(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusNotFound, message)
}

func unprocessable(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusUnprocessableEntity, message)
}

func internalError(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusInternalServerError, message)
}

func errorJson(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"message": message,
		},
	})
}
