package api

import (
	"errors"
	"log/slog"
	"math"
	"regexp"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

var (
	onlineVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
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
	r.Get("/probe/:online_video_id", h.Probe)
	r.Post("/fetch/:video_id", h.Fetch)
	r.Get("/subtitles/:transcription_id", h.Subtitles)
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
	video, err := h.svc.ProbeVideo(c.UserContext(), onlineVideoID)
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

func (h *VideoHandler) Subtitles(c *fiber.Ctx) error {
	transcriptionIDString := c.Params("transcription_id")
	transcriptionID, err := strconv.ParseInt(transcriptionIDString, 10, 64)
	if err != nil {
		h.log.Info("failed to parse transcriptionID", "param", transcriptionIDString, "err", err)
		return badRequest(c, "invalid transcription_id")
	}
	if transcriptionID < 0 {
		h.log.Info("negative transcriptionID", "transcriptionID", transcriptionID)
		return badRequest(c, "invalid transcription_id")
	}
	seq := c.QueryInt("seq", 0)
	if seq < 0 || seq > math.MaxInt32 {
		h.log.Info("invalid seq", "seq", seq)
		return badRequest(c, "invalid seq")
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
		int32(seq),
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
