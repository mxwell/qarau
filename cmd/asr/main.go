package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/asr"
	"github.com/mxwell/qarau/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadASR()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load ASR config: %v\n", err)
		return err
	}

	logOptions := slog.HandlerOptions{
		Level: cfg.LogLevel,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &logOptions))
	logger.Info("ASR worker starting")

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	target := net.JoinHostPort(cfg.APIHost, fmt.Sprint(cfg.APIPort))
	conn, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		logger.Error("failed to create gRPC connection", "err", err)
		return err
	}
	defer conn.Close()
	logger.Info("created gRPC connection", "target", target)

	client := qarauv1.NewJobServiceClient(conn)

	transcoder, err := asr.NewFfmpegTranscoder(logger, cfg.FfmpegTool)
	if err != nil {
		return err
	}
	transcriber, err := asr.NewVoskTranscriber(logger, cfg.VoskModel)
	if err != nil {
		return err
	}

	worker, err := asr.NewASRWorker(
		client,
		logger,
		cfg.WorkerID,
		cfg.WorkingDir,
		cfg.RemoveFiles,
		transcoder,
		transcriber,
	)
	if err != nil {
		return err
	}

	if err := worker.Loop(ctx); err != nil {
		logger.Error("worker loop failed", "err", err)
		return err
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}
}
