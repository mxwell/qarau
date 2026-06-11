package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("fetch worker starting")

	// TODO: dial api over gRPC, run the worker poll loop (internal/worker),
	// lease fetch jobs, download audio, stream it back via CompleteJob.
}
