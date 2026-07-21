package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/asr"
	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/constants"
	"github.com/mxwell/qarau/internal/eleven"
	"github.com/mxwell/qarau/internal/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func createTranscriber(logger *slog.Logger, cfg config.ASRConfig) (asr.Transcriber, error) {
	if cfg.VoskModel != "" {
		result, err := asr.NewVoskTranscriber(logger, cfg.VoskModel)
		if err == nil {
			logger.Info("created Vosk transcriber", "model", cfg.VoskModel)
		}
		return result, err
	} else if cfg.ElevenApiKey != "" {
		result, err := eleven.NewElevenTranscriber(logger, cfg.ElevenApiKey, false)
		if err == nil {
			logger.Info("created Eleven transcriber")
		}
		return result, err
	} else {
		return nil, errors.New("no transcriber configured")
	}
}

func runAsTestTool(ctx context.Context, logger *slog.Logger, cfg config.ASRConfig) error {
	if cfg.ElevenApiKey == "" {
		return fmt.Errorf("test tools needs Eleven API key")
	}
	transcriber, err := eleven.NewElevenTranscriber(logger, cfg.ElevenApiKey, true)
	if err != nil {
		return err
	}
	pcmReader, err := os.Open(cfg.PcmFile)
	if err != nil {
		return err
	}
	defer pcmReader.Close()

	_, err = transcriber.Transcribe(ctx, pcmReader, 30, constants.PcmSampleRate)
	if err != nil {
		return fmt.Errorf("transcribe failed: %w", err)
	}
	logger.Info("transcribe success")

	return nil
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadASR()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger, logCloser, err := logging.NewDailyWriter(cfg.LogDir, "asr", cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logCloser.Close()

	if cfg.PcmFile != "" {
		logger.Info("running as test tool")
		return runAsTestTool(ctx, logger, cfg)
	}

	logger.Info("ASR worker starting")

	certificate, err := tls.LoadX509KeyPair(cfg.MTlsCert, cfg.MTlsKey)
	if err != nil {
		return fmt.Errorf("failed to load cert/key pair: %w", err)
	}

	caCertData, err := os.ReadFile(cfg.MTlsCaCert)
	if err != nil {
		return fmt.Errorf("failed to load CA cert: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertData) {
		return errors.New("failed to append the CA certificate to CA pool")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		RootCAs:      caPool,
	}

	target := net.JoinHostPort(cfg.APIHost, fmt.Sprint(cfg.APIPort))
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
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
	transcriber, err := createTranscriber(logger, cfg)
	if err != nil {
		return err
	}

	worker, err := asr.NewASRWorker(
		client,
		logger,
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
