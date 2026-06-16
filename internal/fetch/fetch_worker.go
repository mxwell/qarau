package fetch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type LoopAction int

const (
	WaitNoJob LoopAction = iota
	BackoffOnError
	FetchVideo
)

type FetchWorker struct {
	client   qarauv1.JobServiceClient
	logger   *slog.Logger
	workerId string
}

func NewFetchWorker(client qarauv1.JobServiceClient, logger *slog.Logger, workerId string) (*FetchWorker, error) {
	if client == nil {
		return nil, errors.New("nil client in FetchWorker creation")
	}
	if logger == nil {
		return nil, errors.New("nil logger in FetchWorker creation")
	}
	if len(workerId) == 0 {
		return nil, errors.New("empty worker ID in FetchWorker creation")
	}
	return &FetchWorker{
		client:   client,
		logger:   logger,
		workerId: workerId,
	}, nil
}

func (fw FetchWorker) claimJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (LoopAction, string) {
	fw.logger.Info("trying to claim job")
	response, err := fw.client.LeaseJob(ctx, request)
	if err != nil {
		fw.logger.Error("lease request failed", "err", err)
		return BackoffOnError, ""
	}
	job := response.Job
	if job == nil {
		return WaitNoJob, ""
	}
	if job.Type != request.Type {
		fw.logger.Error("claimed job has wrong type", "type", job.Type, "video_id", job.VideoId)
		return BackoffOnError, ""
	}
	fetchJob := job.Payload.GetFetchJob()
	if fetchJob == nil {
		fw.logger.Error("no fetch job in response", "type", job.Type)
		return BackoffOnError, ""
	}
	onlineVideoId := fetchJob.OnlineVideoId
	if len(onlineVideoId) == 0 {
		fw.logger.Error("empty OnlineVideoId", "video_id", job.VideoId)
		return BackoffOnError, ""
	}
	return FetchVideo, onlineVideoId
}

func (fw FetchWorker) fetchAudio(ctx context.Context, onlineVideoId string) error {
	fw.logger.Info("fetching audio not implemented", "onlineVideoId", onlineVideoId)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}
	return nil
}

func sleep(ctx context.Context, period time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(period):
		return nil
	}
}

func (fw FetchWorker) Loop(ctx context.Context) error {
	request := qarauv1.LeaseJobRequest{
		Type:     qarauv1.JobType_JOB_TYPE_FETCH,
		WorkerId: fw.workerId,
	}

	initialBackOffPeriod := time.Minute
	maxBackOffPeriod := 10 * time.Minute
	backOffPeriod := initialBackOffPeriod

	pollPeriod := 30 * time.Second

	for {
		action, onlineVideoId := fw.claimJob(ctx, &request)
		switch action {
		case WaitNoJob:
			period := max(pollPeriod, 5*time.Second)
			if err := sleep(ctx, period); err != nil {
				return err
			}
			backOffPeriod = initialBackOffPeriod
			continue
		case BackoffOnError:
			period := max(backOffPeriod, 5*time.Second)
			fw.logger.Info("backoff after error", "period", period)
			if err := sleep(ctx, period); err != nil {
				return err
			}
			backOffPeriod = min(backOffPeriod*2, maxBackOffPeriod)
			continue
		default:
		}
		fw.logger.Info("claimed fetch job", "onlineVideoId", onlineVideoId)
		// TODO: download audio, stream it back via CompleteJob
		if err := fw.fetchAudio(ctx, onlineVideoId); err != nil {
			return err
		}
	}
}
