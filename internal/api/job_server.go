package api

import (
	"bytes"
	"context"
	"hash/crc32"
	"io"
	"log/slog"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type JobServer struct {
	qarauv1.UnimplementedJobServiceServer
	log     *slog.Logger
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
		log:     log,
		service: service,
	}
}

var _ qarauv1.JobServiceServer = (*JobServer)(nil)

func (s *JobServer) LeaseJob(ctx context.Context, request *qarauv1.LeaseJobRequest) (*qarauv1.LeaseJobResponse, error) {
	return s.service.LeaseJob(ctx, request)
}

func (s *JobServer) CompleteFetch(stream grpc.ClientStreamingServer[qarauv1.CompleteFetchRequest, qarauv1.CompleteFetchResponse]) error {
	accumulator := bytes.Buffer{}

	var workerID string
	var jobID int64
	var filename string
	var declaredLen int64 // zero value here means no meta was received, set exactly once on the first meta message arrival

	const maxAudioSize = 200_000_000

	for {
		request, err := stream.Recv()
		if err == io.EOF {
			if declaredLen == 0 {
				s.log.Error("CompleteFetch stream ended before metadata received")
				return status.Errorf(codes.FailedPrecondition, "missing metadata")
			}
			if int64(accumulator.Len()) != declaredLen {
				s.log.Error("accumulator mismatched declared length in CompleteFetch stream", "declaredLen", declaredLen, "len", accumulator.Len())
				return status.Errorf(codes.FailedPrecondition, "incomplete audio data")
			}
			if workerID == "" {
				return status.Errorf(codes.FailedPrecondition, "empty workerID")
			}
			if jobID == 0 {
				return status.Errorf(codes.FailedPrecondition, "empty jobID")
			}
			if filename == "" {
				return status.Errorf(codes.FailedPrecondition, "empty filename")
			}
			err = s.service.CompleteFetchJob(stream.Context(), workerID, jobID, accumulator.Bytes(), filename)
			if err != nil {
				s.log.Error("failed to complete fetch job", "err", err, "job", jobID)
				sendErr := stream.SendAndClose(&qarauv1.CompleteFetchResponse{
					Ok:           false,
					ErrorMessage: "failed to complete fetch job",
				})
				if sendErr != nil {
					s.log.Error("failed to send response with error", "err", err)
				}
				return status.Errorf(codes.Internal, "failed to complete fetch job")
			} else {
				s.log.Info("completed fetch job", "job", jobID)
				sendErr := stream.SendAndClose(&qarauv1.CompleteFetchResponse{
					Ok: true,
				})
				if sendErr != nil {
					s.log.Error("failed to send response with success", "err", err)
				}
				return nil
			}
		} else if err != nil {
			s.log.Error("error in CompleteFetch stream", "err", err)
			return status.Errorf(codes.Unavailable, "failed to receive message")
		}
		switch m := request.GetMsg().(type) {
		case *qarauv1.CompleteFetchRequest_Meta:
			if declaredLen > 0 {
				s.log.Error("repeated meta message in CompleteFetch stream", "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "repeated metadata")
			}
			workerID = m.Meta.WorkerId
			jobID = m.Meta.JobId
			filename = m.Meta.Filename
			declaredLen = m.Meta.Length
			if declaredLen == 0 {
				s.log.Error("zero audio length not allowed", "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "zero audio length not allowed")
			}
			if declaredLen > maxAudioSize {
				s.log.Error("too big audio in CompleteFetch stream", "declaredLen", declaredLen, "job", jobID, "worker", workerID)
				return status.Errorf(codes.FailedPrecondition, "too big audio not allowed")
			}
			s.log.Info("preparing to accept fetched audio", "len", declaredLen, "filename", filename, "job", jobID, "worker", workerID)
			accumulator.Grow(int(declaredLen))
		case *qarauv1.CompleteFetchRequest_Chunk:
			if declaredLen == 0 {
				s.log.Error("audio chunk arrived before metadata")
				return status.Errorf(codes.FailedPrecondition, "audio chunk arrived without metadata")
			}
			localSize := accumulator.Len()
			offset := m.Chunk.Offset
			if int64(localSize) != offset {
				s.log.Error("chunk offset mismatch", "localSize", localSize, "offset", offset)
				return status.Errorf(codes.FailedPrecondition, "audio chunk offset mismatch")
			}
			calculated := crc32.ChecksumIEEE(m.Chunk.Content)
			if calculated != m.Chunk.Crc32 {
				s.log.Error("chunk checksum mismatch", "calculated", calculated, "received", m.Chunk.Crc32, "job", jobID, "worker", workerID)
				return status.Error(codes.FailedPrecondition, "chunk checksum mismatch")
			}
			s.log.Info("appending new chunk", "chunk", len(m.Chunk.Content), "prev_size", localSize)
			accumulator.Write(m.Chunk.Content)
		}
	}
}
