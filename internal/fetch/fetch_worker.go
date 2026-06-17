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
	client     qarauv1.JobServiceClient
	logger     *slog.Logger
	workerID   string
	downloader Downloader
}

type claimedJob struct {
	jobID         int64
	onlineVideoId string
}

func NewFetchWorker(client qarauv1.JobServiceClient, logger *slog.Logger, workerID string, downloader Downloader) (*FetchWorker, error) {
	if client == nil {
		return nil, errors.New("nil client in FetchWorker creation")
	}
	if logger == nil {
		return nil, errors.New("nil logger in FetchWorker creation")
	}
	if len(workerID) == 0 {
		return nil, errors.New("empty worker ID in FetchWorker creation")
	}
	if downloader == nil {
		return nil, errors.New("nil downloader in FetchWorker creation")
	}
	return &FetchWorker{
		client:     client,
		logger:     logger,
		workerID:   workerID,
		downloader: downloader,
	}, nil
}

func (fw *FetchWorker) claimJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (LoopAction, claimedJob) {
	fw.logger.Info("trying to claim job")
	response, err := fw.client.LeaseJob(ctx, request)
	if err != nil {
		fw.logger.Error("lease request failed", "err", err)
		return BackoffOnError, claimedJob{}
	}
	job := response.Job
	if job == nil {
		return WaitNoJob, claimedJob{}
	}
	if job.Type != request.Type {
		fw.logger.Error("claimed job has wrong type", "type", job.Type, "video_id", job.VideoId)
		return BackoffOnError, claimedJob{}
	}
	fetchJob := job.Payload.GetFetchJob()
	if fetchJob == nil {
		fw.logger.Error("no fetch job in response", "type", job.Type)
		return BackoffOnError, claimedJob{}
	}
	onlineVideoId := fetchJob.OnlineVideoId
	if len(onlineVideoId) == 0 {
		fw.logger.Error("empty OnlineVideoId", "video_id", job.VideoId)
		return BackoffOnError, claimedJob{}
	}
	return FetchVideo, claimedJob{
		jobID:         job.Id,
		onlineVideoId: onlineVideoId,
	}
}

func (fw *FetchWorker) fetchAudio(ctx context.Context, job claimedJob) error {
	audioPath, cleanup, err := fw.downloader.Download(ctx, job.jobID, job.onlineVideoId)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fw.logger.Error("download failed", "err", err)
		// TODO report fail to API
		return nil // the error doesn't show up in the main loop as it's business as usual
	}
	fw.logger.Info("downloaded audio", "path", audioPath)
	// TODO stream audio to API
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

func (fw *FetchWorker) Loop(ctx context.Context) error {
	request := qarauv1.LeaseJobRequest{
		Type:     qarauv1.JobType_JOB_TYPE_FETCH,
		WorkerId: fw.workerID,
	}

	initialBackOffPeriod := time.Minute
	maxBackOffPeriod := 10 * time.Minute
	backOffPeriod := initialBackOffPeriod

	pollPeriod := 30 * time.Second

	for {
		action, job := fw.claimJob(ctx, &request)
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
		fw.logger.Info("claimed fetch job", "id", job.jobID, "onlineVideoId", job.onlineVideoId)
		if err := fw.fetchAudio(ctx, job); err != nil {
			return err
		}
	}
}
