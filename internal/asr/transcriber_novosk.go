//go:build !vosk

package asr

import (
	"errors"
	"log/slog"
)

// Stub if built without "vosk" build tag
func NewVoskTranscriber(logger *slog.Logger, modelPath string) (Transcriber, error) {
	return nil, errors.New("vosk support not built into this binary; rebuild with -tags vosk")
}
