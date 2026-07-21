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

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/worker"
)

type FetchWorker struct {
	client       qarauv1.JobServiceClient
	logger       *slog.Logger
	leaseRequest qarauv1.LeaseJobRequest
	downloader   Downloader
}

type claimedJob struct {
	jobID         int64
	videoID       int64
	onlineVideoId string
}

func (j claimedJob) GetID() int64 {
	return j.jobID
}

func NewFetchWorker(client qarauv1.JobServiceClient, logger *slog.Logger, downloader Downloader) (*FetchWorker, error) {
	if client == nil {
		return nil, errors.New("nil client in FetchWorker creation")
	}
	if logger == nil {
		return nil, errors.New("nil logger in FetchWorker creation")
	}
	if downloader == nil {
		return nil, errors.New("nil downloader in FetchWorker creation")
	}
	return &FetchWorker{
		client: client,
		logger: logger,
		leaseRequest: qarauv1.LeaseJobRequest{
			Type: qarauv1.JobType_JOB_TYPE_FETCH,
		},
		downloader: downloader,
	}, nil
}

func (fw *FetchWorker) Claim(ctx context.Context) (worker.LoopAction, claimedJob) {
	response, err := fw.client.LeaseJob(ctx, &fw.leaseRequest)
	if err != nil {
		fw.logger.Error("lease request failed", "err", err)
		return worker.BackoffOnError, claimedJob{}
	}
	job := response.Job
	if job == nil {
		return worker.WaitNoJob, claimedJob{}
	}
	if job.Id <= 0 {
		fw.logger.Error("invalid job ID", "id", job.Id)
		return worker.BackoffOnError, claimedJob{}
	}
	fetchJob := job.Payload.GetFetchJob()
	if fetchJob == nil {
		fw.logger.Error("no fetch job in response", "type", job.Type)
		return worker.BackoffOnError, claimedJob{}
	}
	onlineVideoId := fetchJob.OnlineVideoId
	if len(onlineVideoId) == 0 {
		fw.logger.Error("empty OnlineVideoId", "video_id", job.VideoId)
		return worker.BackoffOnError, claimedJob{}
	}
	return worker.ProcessJob, claimedJob{
		jobID:         job.Id,
		videoID:       job.VideoId,
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
	length := info.Size()
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
				VideoId:  job.videoID,
				Filename: filename,
				Length:   length,
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
	sentChunks := 0
	for offset < length {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			chunkBytes := buf[:n]
			checksum := crc32.ChecksumIEEE(chunkBytes)

			if offset+int64(n) == length || sentChunks%10 == 0 {
				fw.logger.Info("sending audio chunk", "job", job.jobID, "size", n, "offset", offset, "chunk", sentChunks)
			}
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
			sentChunks += 1
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return fmt.Errorf("failed to read audio file: %w", err)
		}
	}
	if length != offset {
		fw.logger.Error("sent length mismatch", "audioSize", length, "sent", offset)
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
		fw.logger.Info("audio sent successfully", "size", offset, "chunks", sentChunks, "job", job.jobID)
	} else {
		fw.logger.Error("audio stream response with error", "error_message", reply.ErrorMessage, "job", job.jobID)
		return retriableError{
			internal: errors.New("error response from stream"),
		}
	}
	return nil
}

func (fw *FetchWorker) failFetch(ctx context.Context, job claimedJob, errorMessage string) error {
	_, err := fw.client.FailJob(ctx, &qarauv1.FailJobRequest{
		JobId:        job.jobID,
		ErrorMessage: errorMessage,
	})
	if err != nil {
		fw.logger.Error("FailJob request failed", "err", err)
		return err
	}
	fw.logger.Info("FailJob request sent", "job", job.jobID)
	return nil
}

func (fw *FetchWorker) Process(ctx context.Context, job claimedJob) error {
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
		errorMessage := fmt.Sprintf("download failed: %v", err)
		if failErr := fw.failFetch(ctx, job, errorMessage); failErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
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

func (fw *FetchWorker) Loop(ctx context.Context) error {
	return worker.Loop(ctx, fw.logger, fw)
}
