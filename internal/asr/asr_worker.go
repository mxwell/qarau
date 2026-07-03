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
)

type ASRWorker struct {
	client       qarauv1.JobServiceClient
	logger       *slog.Logger
	workerID     string
	workingDir   string
	leaseRequest qarauv1.LeaseJobRequest
	transcoder   Transcoder
	transcriber  Transcriber
}

const (
	maxAudioSize = 200_000_000
	sampleRate   = 16_000
)

type claimedJob struct {
	jobID   int64
	videoID int64
}

func (j claimedJob) GetID() int64 {
	return j.jobID
}

func NewASRWorker(
	client qarauv1.JobServiceClient,
	logger *slog.Logger,
	workerID string,
	workingDir string,
	transcoder Transcoder,
	transcriber Transcriber,
) (*ASRWorker, error) {
	if client == nil {
		return nil, errors.New("nil client in ASRWorker creation")
	}
	if logger == nil {
		return nil, errors.New("nil logger in ASRWorker creation")
	}
	if len(workerID) == 0 {
		return nil, errors.New("empty worker ID in ASRWorker creation")
	}
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		logger.Error("failed to create directory for audio files", "dir", workingDir, "err", err)
		return nil, fmt.Errorf("working dir error: %w", err)
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
		transcoder:  transcoder,
		transcriber: transcriber,
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

type fetchedAudio struct {
	path         string
	durationSecs int32
}

func (aw *ASRWorker) getFetchedAudio(ctx context.Context, job claimedJob) (audio fetchedAudio, err error) {
	request := qarauv1.GetFetchedAudioRequest{
		WorkerId: aw.workerID,
		JobId:    job.jobID,
		VideoId:  job.videoID,
	}
	stream, err := aw.client.GetFetchedAudio(ctx, &request)
	if err != nil {
		aw.logger.Error("GetFetchedAudio request failed", "job", job.jobID, "err", err)
		return fetchedAudio{}, err
	}
	defer stream.CloseSend()

	// Getting metadata
	metaResponse, err := stream.Recv()
	if err != nil {
		aw.logger.Error("stream reading fail", "job", job.jobID, "err", err)
		return fetchedAudio{}, fmt.Errorf("stream read error: %w", err)
	}
	if metaResponse == nil || metaResponse.GetMeta() == nil {
		aw.logger.Error("empty meta message", "job", job.jobID)
		return fetchedAudio{}, errors.New("empty meta message")
	}

	declaredLength := metaResponse.GetMeta().Length
	if declaredLength < 1024 {
		aw.logger.Error("too short declared audio", "job", job.jobID, "len", declaredLength)
		return fetchedAudio{}, errors.New("too short audio")
	}
	if declaredLength > maxAudioSize {
		return fetchedAudio{}, fmt.Errorf("declared audio length is too big: %d > %d", declaredLength, maxAudioSize)
	}
	durationSecs := metaResponse.GetMeta().DurationSecs

	filename := metaResponse.GetMeta().Filename
	path := filepath.Join(aw.workingDir, filepath.Base(filename))

	audioFile, err := os.Create(path)
	if err != nil {
		aw.logger.Error("failed to create audio file", "job", job.jobID, "err", err, "path", path)
		return fetchedAudio{}, fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if cerr := audioFile.Close(); cerr != nil {
			aw.logger.Error("failed to close audio file", "job", job.jobID, "err", cerr)
			err = errors.Join(err, fmt.Errorf("failed to close file: %w", cerr))
		}
	}()

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
			return fetchedAudio{}, fmt.Errorf("stream read error: %w", err)
		}
		if response == nil || response.GetChunk() == nil {
			aw.logger.Error("empty chunk message", "job", job.jobID)
			return fetchedAudio{}, fmt.Errorf("empty chunk message")
		}
		chunk := response.GetChunk()
		offset := chunk.Offset
		if offset != written {
			aw.logger.Error("chunk offset mismatch", "written", written, "offset", offset)
			return fetchedAudio{}, fmt.Errorf("audio chunk offset mismatch")
		}
		calculated := crc32.ChecksumIEEE(chunk.Content)
		if calculated != chunk.Crc32 {
			aw.logger.Error("chunk checksum mismatch", "calculated", calculated, "received", chunk.Crc32, "job", job.jobID, "offset", offset)
			return fetchedAudio{}, fmt.Errorf("chunk checksum mismatch")
		}
		chunkSize := int64(len(chunk.Content))
		if offset+chunkSize == declaredLength || receivedChunks%10 == 0 {
			aw.logger.Info("appending new chunk", "job", job.jobID, "size", chunkSize, "offset", offset, "chunk", receivedChunks)
		}
		n, err := audioFile.Write(chunk.Content)
		if err != nil {
			aw.logger.Error("file write error", "job", job.jobID, "err", err)
			return fetchedAudio{}, fmt.Errorf("failed to write to file: %w", err)
		}
		if int64(n) != chunkSize {
			aw.logger.Error("partial file write", "job", job.jobID)
			return fetchedAudio{}, fmt.Errorf("partial file write")
		}
		written += chunkSize
		receivedChunks += 1
		if written > declaredLength {
			aw.logger.Error("audio is longer than declared", "job", job.jobID, "declared", declaredLength, "written", written)
			return fetchedAudio{}, fmt.Errorf("audio is longer than declared")
		}
	}

	if written != declaredLength {
		aw.logger.Error("audio length mismatch", "job", job.jobID, "declared", declaredLength, "written", written)
		return fetchedAudio{}, fmt.Errorf("audio length mismatch")
	}

	aw.logger.Info("written chunks to file", "job", job.jobID, "written", written, "path", path)
	return fetchedAudio{
		path:         path,
		durationSecs: durationSecs,
	}, nil
}

func (aw *ASRWorker) processAudio(ctx context.Context, job claimedJob, audio fetchedAudio) (transcription *qarauv1.Transcription, err error) {
	reader, err := aw.transcoder.Transcode(ctx, audio.path, sampleRate)
	if err != nil {
		aw.logger.Error("transcoder failed", "job", job.jobID, "err", err)
		return nil, err
	}
	defer func() {
		if cerr := reader.Close(); cerr != nil {
			if ctx.Err() != nil {
				aw.logger.Info("transcoding interrupted", "job", job.jobID, "cerr", cerr)
			} else {
				aw.logger.Error("transcoder close failed", "job", job.jobID, "cerr", cerr)
			}
			err = errors.Join(err, cerr)
		}
	}()

	return aw.transcriber.Transcribe(ctx, reader, audio.durationSecs, sampleRate)
}

func (aw *ASRWorker) sendTranscription(ctx context.Context, job claimedJob, transcription *qarauv1.Transcription) error {
	request := &qarauv1.CompleteAsrRequest{
		JobId:         job.jobID,
		VideoId:       job.videoID,
		Transcription: transcription,
		WorkerId:      aw.workerID,
	}

	response, err := aw.client.CompleteAsr(ctx, request)
	if err != nil {
		return fmt.Errorf("CompleteAsr request failed: %w", err)
	}
	if !response.Ok {
		return fmt.Errorf("CompleteAsr response has error: %v", response.ErrorMessage)
	}
	return nil
}

func (aw *ASRWorker) Process(ctx context.Context, job claimedJob) error {
	// TODO cleanup file
	audio, err := aw.getFetchedAudio(ctx, job)
	if err != nil {
		aw.logger.Error("failed to get fetched audio", "job", job.jobID, "err", err)
		// TODO unlock the job
		return nil
	}

	transcription, err := aw.processAudio(ctx, job, audio)
	if err != nil {
		aw.logger.Error("audio processing failed", "job", job.jobID, "err", err)
		return nil
	}
	if transcription == nil {
		aw.logger.Error("nil transcription from processing", "job", job.jobID)
		return nil
	}
	if err := aw.sendTranscription(ctx, job, transcription); err != nil {
		aw.logger.Error("failed to send transcription", "job", job.jobID, "err", err)
		return nil
	}
	aw.logger.Info("transcription sent", "job", job.jobID, "words", len(transcription.Words))
	return nil
}

func (aw *ASRWorker) Loop(ctx context.Context) error {
	return worker.Loop(ctx, aw.logger, aw)
}
