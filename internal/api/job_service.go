package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type JobService struct {
	log     *slog.Logger
	queries *dbgen.Queries
}

func NewService(log *slog.Logger, queries *dbgen.Queries) *JobService {
	if log == nil {
		panic("api: NewService requires a non-nil logger")
	}
	if queries == nil {
		panic("api: NewService requires non-nil queries")
	}
	return &JobService{
		log:     log,
		queries: queries,
	}
}

func convertJobType(protoJobType qarauv1.JobType) (dbgen.JobType, error) {
	switch protoJobType {
	case qarauv1.JobType_JOB_TYPE_ASR:
		return dbgen.JobTypeAsr, nil
	case qarauv1.JobType_JOB_TYPE_FETCH:
		return dbgen.JobTypeFetch, nil
	default:
		return "", fmt.Errorf("unknown job type %v", protoJobType)
	}
}

func prepareJobPayload(jobType qarauv1.JobType, row dbgen.ClaimJobRow) *qarauv1.JobPayload {
	if jobType == qarauv1.JobType_JOB_TYPE_FETCH {
		return &qarauv1.JobPayload{
			Payload: &qarauv1.JobPayload_FetchJob{
				FetchJob: &qarauv1.FetchJob{
					OnlineVideoId: row.OnlineVideoID,
				},
			},
		}
	}
	panic("job types other than fetch are not supported")
}

func (s JobService) LeaseJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	if len(request.WorkerId) == 0 {
		return nil, errors.New("empty worker ID")
	}

	// TODO support ASR jobs
	if request.Type != qarauv1.JobType_JOB_TYPE_FETCH {
		return nil, errors.ErrUnsupported
	}

	jobType, err := convertJobType(request.Type)
	if err != nil {
		return nil, err
	}

	lockPeriod := 5 * time.Minute
	if jobType == dbgen.JobTypeAsr {
		lockPeriod = 15 * time.Minute
	}

	lockedUntil := pgtype.Timestamptz{
		Time:  time.Now().Add(lockPeriod),
		Valid: true,
	}
	arg := dbgen.ClaimJobParams{
		LockedBy:    &request.WorkerId,
		LockedUntil: lockedUntil,
		JobType:     jobType,
	}
	row, err := s.queries.ClaimJob(ctx, arg)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &qarauv1.LeaseJobResponse{}, nil
		}
		return nil, err
	}

	return &qarauv1.LeaseJobResponse{
		Job: &qarauv1.Job{
			Id:      row.ID,
			VideoId: row.VideoID,
			Type:    request.Type,
			Payload: prepareJobPayload(request.Type, row),
		},
	}, nil
}
