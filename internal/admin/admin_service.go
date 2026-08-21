package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	dbgen "github.com/mxwell/qarau/db/gen"
	"github.com/mxwell/qarau/internal/api"
)

type AdminService struct {
	log     *slog.Logger
	queries *dbgen.Queries
}

func NewAdminService(log *slog.Logger, queries *dbgen.Queries) (*AdminService, error) {
	if log == nil {
		return nil, errors.New("nil logger in AdminService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in AdminService creation")
	}
	return &AdminService{
		log:     log,
		queries: queries,
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
