package api

import (
	"errors"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/gofiber/fiber/v2"
	dbgen "github.com/mxwell/qarau/db/gen"
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
	jobs, err := h.svc.GetVideoJobs(c.UserContext(), video.ID)
	if err != nil {
		h.log.Error("failed to load video jobs", "onlineVideoID", onlineVideoID, "err", err)
		return internalError(c, "internal error while checking processing state")
	}
	processingState := jobs.GetProcessingState()
	h.log.Info("checked video processing state", "onlineVideoID", onlineVideoID, "processingState", processingState)
	return probedVideoJSON(c, video, processingState)
}

func probedVideoJSON(c *fiber.Ctx, video *Video, processingState ProcessingState) error {
	return c.JSON(fiber.Map{
		"video": fiber.Map{
			"id":              video.ID,
			"online_video_id": video.OnlineVideoID,
			"title":           video.Title,
			"channel_title":   video.ChannelTitle,
			"duration_secs":   video.DurationSecs,
			"default_lang":    video.DefaultLang,
			"embeddable":      video.Embeddable,
		},
		"processing_obstacle": video.ProcessingObstacle(),
		"processing_state":    processingState,
	})
}

func (h *VideoHandler) Fetch(c *fiber.Ctx) error {
	videoIDParam := c.Params("video_id")
	videoID, err := strconv.ParseInt(videoIDParam, 10, 64)
	if err != nil || videoID <= 0 {
		h.log.Info("invalid argument", "videoID", videoIDParam)
		return badRequest(c, "invalid video_id")
	}
	videoJobs, err := h.svc.CreateOrGetVideoJobs(c.UserContext(), videoID)
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
	h.log.Info("created or loaded jobs for video", "videoID", videoID, "fetch", videoJobs.fetch.jobID, "asr", videoJobs.asr.jobID)
	return videoJobsJson(c, &videoJobs)
}

func videoJobsJson(c *fiber.Ctx, videoJobs *VideoJobs) error {
	return c.JSON(fiber.Map{
		"processing_state": videoJobs.GetProcessingState(),
		"fetch_done":       videoJobs.fetch.state == dbgen.JobStateDone,
		"asr_done":         videoJobs.asr.state == dbgen.JobStateDone,
	})
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
