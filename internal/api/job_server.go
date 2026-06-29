package api

import (
	"bytes"
	"context"
	"hash/crc32"
	"io"
	"log/slog"
	"strings"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxAudioChunk = 1 << 20
)

type JobServer struct {
	qarauv1.UnimplementedJobServiceServer
	logger  *slog.Logger
	service *JobService
}

func NewServer(log *slog.Logger, service *JobService) *JobServer {
	if log == nil {
		panic("api: NewServer requires a non-nil logger")
	}
	if service == nil {
		panic("api: NewServer requires a non-nil service")
	}
	return &JobServer{
		logger:  log,
		service: service,
	}
}

var _ qarauv1.JobServiceServer = (*JobServer)(nil)

func (s *JobServer) LeaseJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	return s.service.LeaseJob(ctx, request)
}

func (s *JobServer) validateFilename(jobID int64, filename string) error {
	if len(filename) <= 3 {
		s.logger.Error("too short filename", "job", jobID, "filename", filename)
		return status.Errorf(codes.FailedPrecondition, "too short filename")
	}
	dotPos := strings.Index(filename, ".")
	if dotPos < 1 || dotPos+1 >= len(filename) {
		s.logger.Error("invalid filename - there should be non-empty fragments around dot", "job", jobID, "filename", filename)
		return status.Errorf(codes.FailedPrecondition, "invalid filename")
	}
	return nil
}

func (s *JobServer) CompleteFetch(stream grpc.ClientStreamingServer[qarauv1.CompleteFetchRequest, qarauv1.CompleteFetchResponse]) error {
	accumulator := bytes.Buffer{}

	var workerID string
	var jobID int64
	var videoID int64
	var filename string
	var declaredLen int64 // zero value here means no meta was received, set exactly once on the first meta message arrival

	const maxAudioSize = 200_000_000

	for {
		request, err := stream.Recv()
		if err == io.EOF {
			if declaredLen == 0 {
				s.logger.Error("CompleteFetch stream ended before metadata received")
				return status.Errorf(codes.FailedPrecondition, "missing metadata")
			}
			if int64(accumulator.Len()) != declaredLen {
				s.logger.Error("accumulator mismatched declared length in CompleteFetch stream", "declaredLen", declaredLen, "len", accumulator.Len())
				return status.Errorf(codes.FailedPrecondition, "incomplete audio data")
			}
			err = s.service.CompleteFetchJob(stream.Context(), workerID, jobID, videoID, accumulator.Bytes(), filename)
			if err != nil {
				s.logger.Error("failed to complete fetch job", "err", err, "job", jobID)
				sendErr := stream.SendAndClose(&qarauv1.CompleteFetchResponse{
					Ok:           false,
					ErrorMessage: "failed to complete fetch job",
				})
				if sendErr != nil {
					s.logger.Error("failed to send response with error", "err", err)
				}
				return status.Errorf(codes.Internal, "failed to complete fetch job")
			} else {
				s.logger.Info("completed fetch job", "job", jobID)
				sendErr := stream.SendAndClose(&qarauv1.CompleteFetchResponse{
					Ok: true,
				})
				if sendErr != nil {
					s.logger.Error("failed to send response with success", "err", err)
				}
				return nil
			}
		} else if err != nil {
			s.logger.Error("error in CompleteFetch stream", "err", err)
			return status.Errorf(codes.Unavailable, "failed to receive message")
		}
		switch m := request.GetMsg().(type) {
		case *qarauv1.CompleteFetchRequest_Meta:
			if declaredLen > 0 {
				s.logger.Error("repeated meta message in CompleteFetch stream", "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "repeated metadata")
			}

			declaredLen = m.Meta.Length
			workerID = m.Meta.WorkerId
			jobID = m.Meta.JobId
			videoID = m.Meta.VideoId
			filename = m.Meta.Filename

			if declaredLen == 0 {
				s.logger.Error("zero audio length not allowed", "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "zero audio length not allowed")
			}
			if declaredLen > maxAudioSize {
				s.logger.Error("too big audio in CompleteFetch stream", "declaredLen", declaredLen, "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "too big audio not allowed")
			}
			if workerID == "" {
				return status.Errorf(codes.FailedPrecondition, "empty workerID")
			}
			if jobID <= 0 {
				return status.Errorf(codes.FailedPrecondition, "empty jobID")
			}
			if videoID <= 0 {
				return status.Errorf(codes.FailedPrecondition, "empty videoID")
			}
			if filenameErr := s.validateFilename(jobID, filename); filenameErr != nil {
				return filenameErr
			}
			s.logger.Info("preparing to accept fetched audio", "len", declaredLen, "filename", filename, "job", jobID, "worker", workerID)
			accumulator.Grow(int(declaredLen))
		case *qarauv1.CompleteFetchRequest_Chunk:
			if declaredLen == 0 {
				s.logger.Error("audio chunk arrived before metadata")
				return status.Errorf(codes.FailedPrecondition, "audio chunk arrived without metadata")
			}
			localSize := accumulator.Len()
			offset := m.Chunk.Offset
			if int64(localSize) != offset {
				s.logger.Error("chunk offset mismatch", "localSize", localSize, "offset", offset)
				return status.Errorf(codes.FailedPrecondition, "audio chunk offset mismatch")
			}
			calculated := crc32.ChecksumIEEE(m.Chunk.Content)
			if calculated != m.Chunk.Crc32 {
				s.logger.Error("chunk checksum mismatch", "calculated", calculated, "received", m.Chunk.Crc32, "job", jobID, "worker", workerID)
				return status.Error(codes.FailedPrecondition, "chunk checksum mismatch")
			}
			s.logger.Info("appending new chunk", "chunk", len(m.Chunk.Content), "prev_size", localSize)
			accumulator.Write(m.Chunk.Content)
		}
	}
}

func (s *JobServer) GetFetchedAudio(request *qarauv1.GetFetchedAudioRequest, stream grpc.ServerStreamingServer[qarauv1.GetFetchedAudioResponse]) error {
	jobID := request.JobId
	leaseValid := s.service.CheckLease(stream.Context(), jobID, request.WorkerId)
	if !leaseValid {
		return status.Errorf(codes.NotFound, "lease not found")
	}

	videoID := request.VideoId
	audioBlob, err := s.service.GetAudioBlob(stream.Context(), videoID)
	if err != nil {
		return status.Errorf(codes.Internal, "audio not available")
	}

	length := int64(len(audioBlob.Content))
	s.logger.Info("loaded audio blob", "job", jobID, "length", length, "filename", audioBlob.Filename)

	err = stream.Send(&qarauv1.GetFetchedAudioResponse{
		Msg: &qarauv1.GetFetchedAudioResponse_Meta{
			Meta: &qarauv1.FetchedAudioMeta{
				Filename:     audioBlob.Filename,
				Length:       length,
				DurationSecs: audioBlob.DurationSecs,
			},
		},
	})
	if err != nil {
		s.logger.Error("failed to send audio metadata", "job", jobID, "err", err)
		return status.Errorf(codes.Unavailable, "failed to send metadata")
	}

	offset := int64(0)
	sentChunks := 0

	for offset < length {
		chunkSize := min(maxAudioChunk, length-offset)
		content := audioBlob.Content[offset : offset+chunkSize]
		checksum := crc32.ChecksumIEEE(content)
		chunkMessage := qarauv1.FetchedAudioChunk{
			Offset:  offset,
			Content: content,
			Crc32:   checksum,
		}

		if offset+chunkSize == length || sentChunks%10 == 0 {
			s.logger.Info("sending audio chunk", "job", jobID, "size", chunkSize, "offset", offset, "chunk", sentChunks)
		}
		err = stream.Send(&qarauv1.GetFetchedAudioResponse{
			Msg: &qarauv1.GetFetchedAudioResponse_Chunk{
				Chunk: &chunkMessage,
			},
		})
		if err != nil {
			s.logger.Error("failed to send audio chunk", "job", jobID, "err", err, "offset", offset, "size", chunkSize)
			return err
		}
		offset += chunkSize
		sentChunks += 1
	}

	if offset != length {
		s.logger.Error("sent length mismatch", "job", jobID, "offset", offset, "length", length)
		return status.Errorf(codes.Internal, "audio size mismatch")
	}

	s.logger.Info("audio sent successfully", "job", jobID, "length", length, "chunks", sentChunks)
	return nil
}

func (s *JobServer) FailJob(ctx context.Context, request *qarauv1.FailJobRequest) (*qarauv1.FailJobResponse, error) {
	s.logger.Info("received FailJob request", "job", request.JobId, "worker", request.WorkerId, "errorMessage", request.ErrorMessage)
	if err := s.service.FailJob(ctx, request.JobId, request.WorkerId, request.ErrorMessage); err != nil {
		return &qarauv1.FailJobResponse{}, status.Error(codes.Internal, "failed to mark the job failed")
	}
	return &qarauv1.FailJobResponse{}, nil
}
