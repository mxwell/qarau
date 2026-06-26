package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type ASRConfig struct {
	WorkerID   string
	APIHost    string
	APIPort    uint16
	WorkingDir string
	LogLevel   slog.Level
}

func LoadASR() (ASRConfig, error) {
	v := viper.New()

	v.SetConfigFile(".asr.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	v.SetDefault("API_HOST", "localhost")
	v.SetDefault("API_PORT", 7991)
	v.SetDefault("LOG_LEVEL", "INFO")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ASRConfig{}, fmt.Errorf("ASR config error: %w", err)
	}

	logLevel, err := parseLogLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return ASRConfig{}, fmt.Errorf("ASR config error: %w", err)
	}

	cfg := ASRConfig{
		WorkerID:   v.GetString("WORKER_ID"),
		APIHost:    v.GetString("API_HOST"),
		APIPort:    v.GetUint16("API_PORT"),
		WorkingDir: v.GetString("WORKING_DIR"),
		LogLevel:   logLevel,
	}

	if !strings.HasPrefix(cfg.WorkerID, "asr_") {
		return ASRConfig{}, fmt.Errorf("ASR config error: worker ID should start with asr_ - '%v'", cfg.WorkerID)
	}

	if len(cfg.APIHost) == 0 {
		return ASRConfig{}, errors.New("ASR config error: empty API host")
	}
	if cfg.APIPort < 1024 {
		return ASRConfig{}, fmt.Errorf("ASR config error: invalid API port %d", cfg.APIPort)
	}
	if !filepath.IsAbs(cfg.WorkingDir) {
		return ASRConfig{}, fmt.Errorf("ASR config error: invalid working dir '%v'", cfg.WorkingDir)
	}

	return cfg, nil
}
