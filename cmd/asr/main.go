package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("asr worker starting")

	// TODO: dial api over gRPC, run the worker poll loop (internal/worker),
	// lease asr jobs, fetch audio via GetAudio, transcribe, report via CompleteJob.
}
