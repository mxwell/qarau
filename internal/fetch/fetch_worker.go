package fetch

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

const (
	maxAudioSize  = 200_000_000
	maxAudioChunk = 1 << 20
)

type retriableError struct {
	internal error
}

func (err retriableError) Error() string {
	return err.internal.Error()
}

func (err retriableError) Unwrap() error {
	return err.internal
}

func (fw *FetchWorker) streamAudio(ctx context.Context, job claimedJob, audioPath string) error {
	f, err := os.Open(audioPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	if info.Size() > maxAudioSize {
		return fmt.Errorf("audio is too big to stream - %d", info.Size())
	}
	audioSize := info.Size()
	filename := filepath.Base(audioPath)
	stream, err := fw.client.CompleteFetch(ctx)
	if err != nil {
		return retriableError{
			internal: fmt.Errorf("failed to initiate a stream for audio: %w", err),
		}
	}

	err = stream.Send(&qarauv1.CompleteFetchRequest{
		Msg: &qarauv1.CompleteFetchRequest_Meta{
			Meta: &qarauv1.CompleteFetchMeta{
				JobId:    job.jobID,
				Filename: filename,
				Length:   audioSize,
				WorkerId: fw.workerID,
			},
		},
	})
	if err != nil {
		return retriableError{
			internal: fmt.Errorf("failed to send meta: %w", err),
		}
	}

	buf := make([]byte, maxAudioChunk)
	offset := int64(0)
	sent := 0
	for offset < audioSize {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			chunkBytes := buf[:n]
			checksum := crc32.ChecksumIEEE(chunkBytes)
			fw.logger.Info("sending chunk", "size", n, "offset", offset)
			sendErr := stream.Send(&qarauv1.CompleteFetchRequest{
				Msg: &qarauv1.CompleteFetchRequest_Chunk{
					Chunk: &qarauv1.CompleteFetchChunk{
						Offset:  offset,
						Content: chunkBytes,
						Crc32:   checksum,
					},
				},
			})
			if sendErr != nil {
				return retriableError{
					internal: fmt.Errorf("failed to send chunk: %w", sendErr),
				}
			}
			offset += int64(n)
			sent += 1
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return fmt.Errorf("failed to read audio file: %w", err)
		}
	}
	if audioSize != offset {
		fw.logger.Error("sent length mismatch", "audioSize", audioSize, "sent", offset)
		return errors.New("send audio length mismatch")
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		fw.logger.Error("failed to close stream", "err", err)
		return retriableError{
			internal: errors.New("failed to close stream"),
		}
	}
	if reply.Ok {
		fw.logger.Info("audio sent successfully", "size", offset, "chunks", sent, "job", job.jobID)
	} else {
		fw.logger.Error("audio stream response with error", "error_message", reply.ErrorMessage, "job", job.jobID)
		return retriableError{
			internal: errors.New("error response from stream"),
		}
	}
	return nil
}

/*
 * fetchAudio only returns errors that should terminate the whole worker
 */
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
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt += 1 {
		if attempt > 0 {
			fw.logger.Info("another attempt to stream audio", "attempt", attempt)
		}
		err = fw.streamAudio(ctx, job, audioPath)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var retriable retriableError
			if errors.As(err, &retriable) && attempt+1 < maxAttempts {
				fw.logger.Error("retriable error during stream", "err", retriable)
			} else {
				fw.logger.Error("failed to stream audio", "err", err)
				break
			}
		} else {
			break
		}
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
