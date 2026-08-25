package admin

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/mxwell/qarau/internal/api"
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
