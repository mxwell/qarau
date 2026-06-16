package config

import (
	"fmt"
	"log/slog"
	"strings"
)

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToUpper(s) {
	case slog.LevelDebug.String():
		return slog.LevelDebug, nil
	case slog.LevelInfo.String():
		return slog.LevelInfo, nil
	case slog.LevelWarn.String():
		return slog.LevelWarn, nil
	case slog.LevelError.String():
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("unknown log level '%v'", s)
}
