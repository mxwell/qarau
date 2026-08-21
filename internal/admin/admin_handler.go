package admin

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/mxwell/qarau/internal/fiberutil"
)

type AdminHandler struct {
	log *slog.Logger
	svc *AdminService
}

func NewAdminHandler(log *slog.Logger, svc *AdminService) (*AdminHandler, error) {
	if log == nil {
		return nil, errors.New("nil logger in AdminHandler creation")
	}
	if svc == nil {
		return nil, errors.New("nil svc in AdminHandler creation")
	}
	return &AdminHandler{
		log: log,
		svc: svc,
	}, nil
}

func (h *AdminHandler) Register(r fiber.Router) {
	r.Get("/jobs", h.ListJobs)
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
