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
	"sync"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/getsentry/sentry-go/fiber"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/mxwell/qarau/db/gen"
	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
	"github.com/mxwell/qarau/internal/admin"
	"github.com/mxwell/qarau/internal/api"
	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/llm"
	"github.com/mxwell/qarau/internal/logging"
	"github.com/mxwell/qarau/internal/quota"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadAPI()
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
		"api",
		cfg.LogLevel,
		altHandler,
	)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}
	defer logCloser.Close()

	logger.Info("api starting")
	if sentryInitialized {
		logger.Info("sentry initialized")
	}

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
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    caPool,
	}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(api.GrpcUnarySentryInterceptor),
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)

	queries := dbgen.New(db)

	sentMan, err := api.NewSentenceManager(logger, queries, db)
	if err != nil {
		logger.Error("failed to create SentenceManager", "err", err)
		return err
	}

	jobService, err := api.NewService(logger, queries, db)
	if err != nil {
		logger.Error("failed to create JobService", "err", err)
		return err
	}
	jobServer, err := api.NewJobServer(logger, jobService, sentMan)
	if err != nil {
		logger.Error("failed to create JobServer", "err", err)
		return err
	}
	qarauv1.RegisterJobServiceServer(grpcServer, jobServer)

	// Fiber app
	sentryHandler := sentryfiber.New(sentryfiber.Options{
		Repanic:         true,
		WaitForDelivery: true,
	})
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(
		sentryHandler,
		logging.FiberRequestLogger(logger),
	)

	ytClient, err := api.NewYtClient(ctx, logger, cfg.YTApiKey)
	if err != nil {
		logger.Error("failed to create YouTube API client", "err", err)
		return err
	}
	llmQuotaCtl, err := quota.NewLlmQuotaController(cfg.LlmInPrice, cfg.LlmOutPrice, cfg.LlmDaily)
	if err != nil {
		logger.Error("failed to create LLM quota controller", "err", err)
		return err
	}
	videoService, err := api.NewVideoService(logger, queries, db, ytClient, llmQuotaCtl)
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

	if adminToken := cfg.AdminToken; adminToken != "" {
		adminService, err := admin.NewAdminService(logger, queries)
		if err != nil {
			logger.Error("failed to create AdminService", "err", err)
			return err
		}

		adminHandler, err := admin.NewAdminHandler(logger, adminService, sentMan)
		if err != nil {
			logger.Error("failed to create AdminHandler")
			return err
		}

		adminRouter := app.Group(
			"/qarauadmin/api/v1",
			admin.AdminAuth(adminToken),
		)
		adminHandler.Register(adminRouter)
	}

	oaiClient, err := llm.New(logger, cfg.LlmApiKey)
	if err != nil {
		logger.Error("failed to create OpenAI client", "err", err)
		return err
	}
	bbRunner, err := llm.NewBBRunner(
		logger,
		queries,
		db,
		llmQuotaCtl,
		oaiClient,
		cfg.LlmPrompt,
	)
	if err != nil {
		logger.Error("failed to create BBRunner", "err", err)
		return err
	}

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

	group.Go(func() error {
		logger.Info("starting BBRunner")
		if err := bbRunner.Loop(groupCtx); err != nil {
			if errors.Is(err, llm.ErrCancelFromContext) {
				logger.Info("BBRunner stopped by signal from context")
			} else {
				logger.Error("BBRunner failed", "err", err)
				return err
			}
		} else {
			logger.Info("BBRunner stopped")
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
