package api

import (
	"errors"
	"log/slog"
	"regexp"

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
	// TODO get processingState (new, pending, started, finished, failed)
	return probedVideoJSON(c, video, "unknown")
}

func probedVideoJSON(c *fiber.Ctx, video *Video, processingState string) error {
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

func badRequest(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusBadRequest, message)
}

func notFound(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusNotFound, message)
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
