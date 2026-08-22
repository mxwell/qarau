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
	"time"

	"github.com/getsentry/sentry-go"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/fetch"
	"github.com/mxwell/qarau/internal/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadFetch()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var altHandler slog.Handler
	sentryInitialized := false
	if cfg.SentryDSN != "" {
		err = sentry.Init(sentry.ClientOptions{Dsn: cfg.SentryDSN})
		if err != nil {
			return fmt.Errorf("sentry init fail: %w", err)
		}
		defer sentry.Flush(2 * time.Second)
		altHandler = logging.NewSentryIssueHandler()
		sentryInitialized = true
	}

	logger, logCloser, err := logging.NewDailyWriter(
		cfg.LogDir,
		"fetch",
		cfg.LogLevel,
		altHandler,
	)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logCloser.Close()

	logger.Info("fetch starting")
	if sentryInitialized {
		logger.Info("sentry initialized")
	}

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

	downloader, err := fetch.NewYtDlpDownloader(logger, cfg.Tool, cfg.GetJsRuntime(), cfg.WorkingDir)
	if err != nil {
		return err
	}

	worker, err := fetch.NewFetchWorker(
		client,
		logger,
		downloader,
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
