package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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

func NewDailyWriter(dir, component string, level slog.Level) (*slog.Logger, io.Closer, error) {
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

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(handler), w, nil
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
