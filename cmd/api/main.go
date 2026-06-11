package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("api starting")

	// TODO: load config (internal/config), connect to Postgres (db),
	// start the gRPC JobService + REST (fiber) servers, handle graceful shutdown.
}
