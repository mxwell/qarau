package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/logging"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger, logCloser, err := logging.NewDailyWriter(cfg.LogDir, "api", cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logCloser.Close()
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

	// gRPC
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)

	queries := dbgen.New(db)
	jobService, err := api.NewService(logger, queries, db)
	if err != nil {
		logger.Error("failed to create JobService", "err", err)
		return err
	}
	qarauv1.RegisterJobServiceServer(
		grpcServer,
		api.NewServer(logger, jobService),
	)

	// Fiber app
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logging.FiberRequestLogger(logger))

	ytClient, err := api.NewYtClient(ctx, logger, cfg.YTApiKey)
	if err != nil {
		logger.Error("failed to create YouTube API client", "err", err)
		return err
	}
	videoService, err := api.NewVideoService(logger, queries, ytClient)
	if err != nil {
		logger.Error("failed to create VideoService", "err", err)
		return err
	}
	videoHandler, err := api.NewVideoHandler(logger, videoService)
	if err != nil {
		logger.Error("failed to create VideoHandler", "err", err)
		return err
	}

	apiRouter := app.Group("/qarauapi/v1")
	videoHandler.Register(apiRouter)

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		<-groupCtx.Done() // wakes on signal or sibling failure

		// launch 2 shutdown procedures (gRPC and Fiber) and wait for both to finish
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			grpcServer.GracefulStop()
			logger.Info("grpc server graceful stop is done")
		}()
		go func() {
			defer wg.Done()
			logger.Info("shutting down app")
			appErr := app.Shutdown()
			if appErr != nil {
				logger.Error("failed to shutdown app", "err", appErr)
			} else {
				logger.Info("fiber app shutdown is done")
			}
		}()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		// waiting for shutdown procedures to end but with a timer
		select {
		case <-done:
			logger.Info("shutdown procedures are done")
		case <-time.After(cfg.GracePeriod):
			logger.Warn("force stop after delay", "grace_period", cfg.GracePeriod)
			grpcServer.Stop()
		}
		return nil
	})

	group.Go(func() error {
		logger.Info("grpc listening", "port", cfg.GRPCPort)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("grpc serve failed", "err", err)
			return err
		}
		return nil
	})

	group.Go(func() error {
		logger.Info("rest api listening", "port", cfg.RESTPort)
		addr := fmt.Sprintf(":%d", cfg.RESTPort)
		if err := app.Listen(addr); err != nil {
			logger.Error("rest api serve failed", "err", err)
			return err
		}
		return nil
	})

	logger.Info("starting group")
	return group.Wait()
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("run failed: %v\n", err.Error())
		os.Exit(1)
	}
}
