package asr

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ASRWorker struct {
	client       qarauv1.JobServiceClient
	logger       *slog.Logger
	workerID     string
	workingDir   string
	leaseRequest qarauv1.LeaseJobRequest
}

type claimedJob struct {
	jobID   int64
	videoID int64
}

func (j claimedJob) GetID() int64 {
	return j.jobID
}

func NewASRWorker(client qarauv1.JobServiceClient, logger *slog.Logger, workerID string, workingDir string) (*ASRWorker, error) {
	if client == nil {
		return nil, errors.New("nil client in ASRWorker creation")
	}
	if logger == nil {
		return nil, errors.New("nil logger in ASRWorker creation")
	}
	if len(workerID) == 0 {
		return nil, errors.New("empty worker ID in ASRWorker creation")
	}
	return &ASRWorker{
		client:     client,
		logger:     logger,
		workerID:   workerID,
		workingDir: workingDir,
		leaseRequest: qarauv1.LeaseJobRequest{
			Type:     qarauv1.JobType_JOB_TYPE_ASR,
			WorkerId: workerID,
		},
	}, nil
}

func (aw *ASRWorker) Claim(ctx context.Context) (worker.LoopAction, claimedJob) {
	response, err := aw.client.LeaseJob(ctx, &aw.leaseRequest)
	if err != nil {
		aw.logger.Error("lease request failed", "err", err)
		return worker.BackoffOnError, claimedJob{}
	}
	job := response.Job
	if job == nil {
		return worker.WaitNoJob, claimedJob{}
	}
	if job.Id <= 0 {
		aw.logger.Error("invalid job ID", "id", job.Id)
		return worker.BackoffOnError, claimedJob{}
	}
	asrJob := job.Payload.GetAsrJob()
	if asrJob == nil {
		aw.logger.Error("no asr job in response", "type", job.Type, "id", job.Id)
		return worker.BackoffOnError, claimedJob{}
	}
	return worker.ProcessJob, claimedJob{
		jobID:   job.Id,
		videoID: job.VideoId,
	}
}

func (aw *ASRWorker) getFetchedAudio(ctx context.Context, job claimedJob) (string, error) {
	request := qarauv1.GetFetchedAudioRequest{
		WorkerId: aw.workerID,
		JobId:    job.jobID,
		VideoId:  job.videoID,
	}
	stream, err := aw.client.GetFetchedAudio(ctx, &request)
	defer stream.CloseSend()
	if err != nil {
		aw.logger.Error("GetFetchedAudio request failed", "job", job.jobID, "err", err)
		return "", err
	}

	// Getting metadata
	metaResponse, err := stream.Recv()
	if err != nil {
		aw.logger.Error("stream reading fail", "job", job.jobID, "err", err)
		return "", status.Errorf(codes.Unavailable, "stream read error")
	}
	if metaResponse == nil || metaResponse.GetMeta() == nil {
		aw.logger.Error("empty meta message", "job", job.jobID)
		return "", status.Errorf(codes.Unavailable, "empty meta message")
	}

	if err = os.MkdirAll(aw.workingDir, 0o755); err != nil {
		aw.logger.Error("failed to create directory for audio file", "dir", aw.workingDir, "err", err)
		return "", status.Errorf(codes.Internal, "filesystem error")
	}

	filename := metaResponse.GetMeta().Filename
	path := filepath.Join(aw.workingDir, filepath.Base(filename))

	audioFile, err := os.Create(path)
	if err != nil {
		aw.logger.Error("failed to create audio file", "job", job.jobID, "err", err, "path", path)
		return "", status.Errorf(codes.Internal, "failed to create file")
	}
	defer audioFile.Close()

	declaredLength := metaResponse.GetMeta().Length
	if declaredLength < 1024 {
		aw.logger.Error("too short declared audio", "job", job.jobID, "len", declaredLength)
		return "", status.Errorf(codes.InvalidArgument, "too short audio")
	}

	written := int64(0)
	receivedChunks := 0

	// Reading chunks
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			aw.logger.Error("stream reading fail", "job", job.jobID, "err", err)
			return "", status.Errorf(codes.Unavailable, "stream read error")
		}
		if response == nil || response.GetChunk() == nil {
			aw.logger.Error("empty chunk message", "job", job.jobID)
			return "", status.Errorf(codes.Unavailable, "empty chunk message")
		}
		chunk := response.GetChunk()
		offset := chunk.Offset
		if offset != written {
			aw.logger.Error("chunk offset mismatch", "written", written, "offset", offset)
			return "", status.Errorf(codes.FailedPrecondition, "audio chunk offset mismatch")
		}
		calculated := crc32.ChecksumIEEE(chunk.Content)
		if calculated != chunk.Crc32 {
			aw.logger.Error("chunk checksum mismatch", "calculated", calculated, "received", chunk.Crc32, "job", job.jobID, "offset", offset)
			return "", status.Errorf(codes.FailedPrecondition, "chunk checksum mismatch")
		}
		chunkSize := int64(len(chunk.Content))
		if offset+chunkSize == declaredLength || receivedChunks%10 == 0 {
			aw.logger.Info("appending new chunk", "job", job.jobID, "size", chunkSize, "offset", offset, "chunk", receivedChunks)
		}
		n, err := audioFile.Write(chunk.Content)
		if err != nil {
			aw.logger.Error("file write error", "job", job.jobID, "err", err)
			return "", status.Errorf(codes.Internal, "failed to write to file")
		}
		if int64(n) != chunkSize {
			aw.logger.Error("partial file write", "job", job.jobID)
			return "", status.Errorf(codes.Internal, "failed to write to file")
		}
		written += chunkSize
		receivedChunks += 1
		if written > declaredLength {
			aw.logger.Error("audio is longer than declared", "job", job.jobID, "declared", declaredLength, "written", written)
			return "", status.Errorf(codes.FailedPrecondition, "longer than declared")
		}
	}

	if written != declaredLength {
		aw.logger.Error("audio length mismatch", "job", job.jobID, "declared", declaredLength, "written", written)
		return "", status.Errorf(codes.FailedPrecondition, "audio length mismatch")
	}

	if err := audioFile.Close(); err != nil {
		aw.logger.Error("failed to close audio file", "job", job.jobID, "err", err)
		return "", fmt.Errorf("failed to close file: %w", err)
	}
	aw.logger.Info("written chunks to file", "job", job.jobID, "written", written, "path", path)
	return path, nil
}

func (aw *ASRWorker) Process(ctx context.Context, job claimedJob) error {
	// TODO cleanup file
	audioPath, err := aw.getFetchedAudio(ctx, job)
	if err != nil {
		aw.logger.Error("failed to get fetched audio", "job", job.jobID, "err", err)
		// TODO unlock the job
		return nil
	}
	aw.logger.Info("see file", "path", audioPath)
	// TODO asr and submit results
	return nil
}

func (aw *ASRWorker) Loop(ctx context.Context) error {
	return worker.Loop(ctx, aw.logger, aw)
}
