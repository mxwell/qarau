package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/api"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// TODO: load config (internal/config), connect to Postgres (db)

	port := 9001
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		logger.Error("failed to listen", "port", port, "err", err)
		return err
	}
	logger.Info("listening", "port", port)

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	qarauv1.RegisterJobServiceServer(grpcServer, api.NewServer(logger))

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		<-groupCtx.Done() // wakes on signal or sibling failure
		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()
		delay := 3 * time.Second
		select {
		case <-done:
			logger.Info("graceful stop finished ok")
		case <-time.After(delay):
			logger.Warn("force stop after delay", "delay", delay)
			grpcServer.Stop()
		}
		return nil
	})

	group.Go(func() error {
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("grpc serve failed", "err", err)
			return err
		}
		return nil
	})

	// TODO start REST (fiber) server

	logger.Info("starting group")
	return group.Wait()
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("api starting")

	if err := run(logger); err != nil {
		logger.Error("run failed", "err", err)
		os.Exit(1)
	}
}
