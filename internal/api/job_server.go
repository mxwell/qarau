package api

import (
	"context"
	"log/slog"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type JobServer struct {
	qarauv1.UnimplementedJobServiceServer
	log     *slog.Logger
	service *JobService
}

func NewServer(log *slog.Logger, service *JobService) *JobServer {
	if log == nil {
		panic("api: NewServer requires a non-nil logger")
	}
	if service == nil {
		panic("api: NewServer requires a non-nil service")
	}
	return &JobServer{
		log:     log,
		service: service,
	}
}

var _ qarauv1.JobServiceServer = (*JobServer)(nil)

func (s JobServer) LeaseJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	return s.service.LeaseJob(ctx, request)
}
