package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type LoopAction int

const (
	WaitNoJob LoopAction = iota
	BackOffError
	FetchVideo
)

type FetchWorker struct {
	Client   qarauv1.JobServiceClient
	Logger   *slog.Logger
	WorkerId string
}

func NewFetchWorker(client qarauv1.JobServiceClient, logger *slog.Logger, workerId string) *FetchWorker {
	if logger == nil {
		fmt.Println("failed to create fetch worker: nil logger")
		return nil
	}
	if len(workerId) == 0 {
		logger.Error("failed to create fetch worker with empty worker ID")
		return nil
	}
	return &FetchWorker{
		Client:   client,
		Logger:   logger,
		WorkerId: workerId,
	}
}

func (fw FetchWorker) claimJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (LoopAction, string) {
	fw.Logger.Info("trying to claim job")
	response, err := fw.Client.LeaseJob(ctx, request)
	if err != nil {
		fw.Logger.Error("lease request failed", "err", err)
		return BackOffError, ""
	}
	job := response.Job
	if job == nil {
		return WaitNoJob, ""
	}
	if job.Type != request.Type {
		fw.Logger.Error("claimed job has wrong type", "type", job.Type, "video_id", job.VideoId)
		return BackOffError, ""
	}
	fetchJob := job.Payload.GetFetchJob()
	if fetchJob == nil {
		fw.Logger.Error("no fetch job in response", "type", job.Type)
		return BackOffError, ""
	}
	onlineVideoId := fetchJob.OnlineVideoId
	if len(onlineVideoId) == 0 {
		fw.Logger.Error("empty OnlineVideoId", "video_id", job.VideoId)
		return BackOffError, ""
	}
	return FetchVideo, fetchJob.OnlineVideoId
}

func (fw FetchWorker) fetchAudio(ctx context.Context, onlineVideoId string) error {
	fw.Logger.Info("fetching audio not implemented", "onlineVideoId", onlineVideoId)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
	}
	return nil
}

func sleepWithContextListening(ctx context.Context, period time.Duration) error {
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
		WorkerId: fw.WorkerId,
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
			if err := sleepWithContextListening(ctx, period); err != nil {
				return err
			}
			backOffPeriod = initialBackOffPeriod
			continue
		case BackOffError:
			period := max(backOffPeriod, 5*time.Second)
			fw.Logger.Info("backoff after error", "period", period)
			if err := sleepWithContextListening(ctx, period); err != nil {
				return err
			}
			backOffPeriod = min(backOffPeriod*2, maxBackOffPeriod)
			continue
		default:
		}
		fw.Logger.Info("claimed fetch job", "onlineVideoId", onlineVideoId)
		// TODO: download audio, stream it back via CompleteJob
		if err := fw.fetchAudio(ctx, onlineVideoId); err != nil {
			return err
		}
	}
}
