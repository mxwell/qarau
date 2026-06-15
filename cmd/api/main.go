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

	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/config"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI()
	if err != nil {
		fmt.Printf("failed to load API config: %v\n", err.Error())
		return err
	}

	logOptions := slog.HandlerOptions{
		Level: cfg.LogLevel,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &logOptions))
	logger.Info("api starting")

	db, err := pgxpool.New(ctx, cfg.DBUrl)
	if err != nil {
		logger.Error("failed to connect to database", "err", err)
		return err
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "err", err)
		return err
	}
	logger.Info("connected to db")

	address := fmt.Sprintf(":%d", cfg.GRPCPort)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("failed to listen", "address", address, "err", err)
		return err
	}
	logger.Info("listening grpc", "port", cfg.GRPCPort)

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)

	queries := dbgen.New(db)
	jobService := api.NewService(logger, queries)
	qarauv1.RegisterJobServiceServer(
		grpcServer,
		api.NewServer(logger, jobService),
	)

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		<-groupCtx.Done() // wakes on signal or sibling failure
		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
			logger.Info("graceful stop finished ok")
		case <-time.After(cfg.GracePeriod):
			logger.Warn("force stop after delay", "grace_period", cfg.GracePeriod)
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
	if err := run(); err != nil {
		fmt.Printf("run failed: %v\n", err.Error())
		os.Exit(1)
	}
}
