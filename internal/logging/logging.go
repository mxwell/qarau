package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/mxwell/qarau/internal/constants"
)

// File log writer with rotation
type dailyWriter struct {
	dir       string
	component string
	extension string

	mu  sync.Mutex
	f   *os.File
	day string // YYYY-MM-DD currently open
}

type sentryIssueHandler struct {
}

func NewSentryIssueHandler() *sentryIssueHandler {
	return &sentryIssueHandler{}
}

func (h *sentryIssueHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

func slogAttrValueToAny(v slog.Value) any {
	switch v.Kind() {
	case slog.KindBool, slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindString:
		return v.Any()
	default:
		return v.String()
	}
}

func (h *sentryIssueHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < slog.LevelError {
		return nil
	}
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetFingerprint([]string{record.Message})

		var wrappedError error
		scopeCtx := sentry.Context{"message": record.Message}

		record.Attrs(func(a slog.Attr) bool {
			v := a.Value.Resolve()
			if a.Key == "err" {
				if err, ok := v.Any().(error); ok {
					wrappedError = err
					return true
				}
			}
			scopeCtx[a.Key] = slogAttrValueToAny(v)
			return true
		})

		scope.SetContext("log", scopeCtx)

		if wrappedError != nil {
			hub.CaptureException(wrappedError)
		} else {
			hub.CaptureMessage(record.Message)
		}
	})
	return nil
}

func (h *sentryIssueHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Incorrect, simplified: ignore attrs, return self
	return h
}

func (h *sentryIssueHandler) WithGroup(name string) slog.Handler {
	// Incorrect, simplified: ignore group, return self
	return h
}

func NewDailyWriter(dir, component string, level slog.Level, altHandler slog.Handler) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create log dir %s: %w", dir, err)
	}

	w := &dailyWriter{
		dir:       dir,
		component: component,
		extension: "jsonl",
	}
	if err := w.rotate(); err != nil {
		return nil, nil, err
	}

	var handler slog.Handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	if altHandler != nil {
		handler = slog.NewMultiHandler(handler, altHandler)
	}
	logger := slog.New(handler)

	// redirect messages from dependency libs to the same JSON files
	slog.SetDefault(logger)
	slog.SetLogLoggerLevel(slog.LevelInfo)

	return logger, w, nil
}

func getToday() string {
	return time.Now().In(constants.KZ_TZ).Format(time.DateOnly)
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	today := getToday()
	if today != w.day {
		if err := w.rotateLocked(today); err != nil {
			return 0, err
		}
	}
	return w.f.Write(p)
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *dailyWriter) rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rotateLocked(getToday())
}

func (w *dailyWriter) rotateLocked(day string) error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	path := filepath.Join(w.dir, fmt.Sprintf("%s_%s.%s", w.component, day, w.extension))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", path, err)
	}
	w.f = f
	w.day = day
	return nil
}
