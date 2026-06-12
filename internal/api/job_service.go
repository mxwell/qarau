package api

import (
	"log/slog"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type JobServer struct {
	qarauv1.UnimplementedJobServiceServer
	log *slog.Logger
}

func NewServer(log *slog.Logger) *JobServer {
	if log == nil {
		panic("api: NewServer requires a non-nil logger")
	}
	return &JobServer{
		log: log,
	}
}

var _ qarauv1.JobServiceServer = (*JobServer)(nil)
