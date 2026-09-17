package admin

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/constants"
	"github.com/mxwell/qarau/internal/fiberutil"
)

type AdminHandler struct {
	log     *slog.Logger
	svc     *AdminService
	sentMan *api.SentenceManager
}

func NewAdminHandler(log *slog.Logger, svc *AdminService, sentMan *api.SentenceManager) (*AdminHandler, error) {
	if log == nil {
		return nil, errors.New("nil logger in AdminHandler creation")
	}
	if svc == nil {
		return nil, errors.New("nil svc in AdminHandler creation")
	}
	if sentMan == nil {
		return nil, errors.New("nil sentMan in AdminHandler creation")
	}
	return &AdminHandler{
		log:     log,
		svc:     svc,
		sentMan: sentMan,
	}, nil
}

func (h *AdminHandler) Register(r fiber.Router) {
	r.Get("/jobs", h.ListJobs)
	r.Post("/backfill_sentences", h.BackfillSentences)
	r.Post("/set_video_topics", h.SetVideoTopics)
}

func AdminAuth(token string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Get("Authorization") != "Bearer "+token {
			return fiberutil.UnauthorizedError(c, "admin auth required")
		}
		return c.Next()
	}
}

type ListJobsResponse struct {
	Jobs []JobListItem `json:"jobs"`
}

func (h *AdminHandler) ListJobs(c *fiber.Ctx) error {
	jobs, err := h.svc.GetJobList(c.UserContext())

	if err != nil {
		h.log.Error("GetJobList fail", "err", err)
		return fiberutil.InternalError(c, "internal error")
	}
	return c.JSON(ListJobsResponse{
		Jobs: jobs,
	})
}

type BackfillSentencesRequest struct {
	TranscriptionID int64 `json:"transcription_id"`
}

type BackfillSentencesResponse struct {
	InsertedSentences int64 `json:"inserted_sentences"`
}

func (h *AdminHandler) BackfillSentences(c *fiber.Ctx) error {
	var req BackfillSentencesRequest
	if err := c.BodyParser(&req); err != nil {
		return fiberutil.BadRequest(c, "body parse error")
	}
	transcriptionID := req.TranscriptionID
	if transcriptionID <= 0 {
		h.log.Info("bad transcriptionID in request", "transcriptionID", transcriptionID, "method", "BackfillSentences")
		return fiberutil.BadRequest(c, "invalid transcription ID")
	}

	insertedSentences, err := h.sentMan.BackfillSentences(c.UserContext(), transcriptionID)
	if err != nil {
		if errors.Is(err, api.ErrNoSuchTranscription) {
			return fiberutil.NotFound(c, "transcription not found")
		}
		return fiberutil.InternalError(c, "internal error")
	}
	return c.JSON(BackfillSentencesResponse{
		InsertedSentences: insertedSentences,
	})
}

type SetVideoTopicsRequest struct {
	OnlineVideoID string   `json:"online_video_id"`
	Topics        []string `json:"topics"`
}

type SetVideoTopicsResponse struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
}

func (h *AdminHandler) SetVideoTopics(c *fiber.Ctx) error {
	var req SetVideoTopicsRequest
	if err := c.BodyParser(&req); err != nil {
		return fiberutil.BadRequest(c, "body parse error")
	}
	onlineVideoID := req.OnlineVideoID
	if !constants.OnlineVideoIDPattern.MatchString(onlineVideoID) {
		h.log.Info("invalid arg in SetVideoTopics request", "online_video_id", onlineVideoID)
		return fiberutil.BadRequest(c, "invalid online_video_id")
	}
	topics := req.Topics
	// 0 topics is fine: it means delete
	if len(topics) > 30 {
		h.log.Info("invalid topic count in SetVideoTopics request", "topics", len(topics))
		return fiberutil.BadRequest(c, "invalid topic count")
	}

	err := h.svc.SetVideoTopics(c.UserContext(), onlineVideoID, topics)
	if err != nil {
		return c.JSON(SetVideoTopicsResponse{
			Ok:      false,
			Message: err.Error(),
		})
	}

	return c.JSON(SetVideoTopicsResponse{
		Ok: true,
	})
}
