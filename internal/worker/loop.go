package worker

import (
	"context"
	"log/slog"
	"time"
)

type LoopAction int

const (
	WaitNoJob LoopAction = iota
	BackoffOnError
	ProcessJob
)

type IdentifiableJob interface {
	GetID() int64
}

type ClaimerWorker[Job IdentifiableJob] interface {
	Claim(ctx context.Context) (LoopAction, Job)
	// Process only returns errors that should terminate the whole worker process
	Process(ctx context.Context, job Job) error
}

func sleep(ctx context.Context, period time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(period):
		return nil
	}
}

func Loop[Job IdentifiableJob](ctx context.Context, logger *slog.Logger, claimerWorker ClaimerWorker[Job]) error {
	initialBackOffPeriod := time.Minute
	maxBackOffPeriod := 10 * time.Minute
	backOffPeriod := initialBackOffPeriod

	pollPeriod := 30 * time.Second

	for {
		logger.Info("trying to claim job")
		action, job := claimerWorker.Claim(ctx)
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
			logger.Info("backoff after error", "period", period)
			if err := sleep(ctx, period); err != nil {
				return err
			}
			backOffPeriod = min(backOffPeriod*2, maxBackOffPeriod)
			continue
		default:
		}
		logger.Info("processing claimed job", "id", job.GetID())
		if err := claimerWorker.Process(ctx, job); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
