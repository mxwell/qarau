package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type FetchConfig struct {
	WorkerId string
	APIHost  string
	APIPort  uint16
	LogLevel slog.Level
}

func LoadFetch() (FetchConfig, error) {
	v := viper.New()

	v.SetConfigFile(".fetch.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	v.SetDefault("API_HOST", "localhost")
	v.SetDefault("API_PORT", 7991)
	v.SetDefault("LOG_LEVEL", "INFO")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return FetchConfig{}, fmt.Errorf("fetch config error: %w", err)
	}

	logLevel, err := parseLogLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return FetchConfig{}, fmt.Errorf("fetch config error: %w", err)
	}

	cfg := FetchConfig{
		WorkerId: v.GetString("WORKER_ID"),
		APIHost:  v.GetString("API_HOST"),
		APIPort:  v.GetUint16("API_PORT"),
		LogLevel: logLevel,
	}

	if !strings.HasPrefix(cfg.WorkerId, "fetch_") {
		return FetchConfig{}, fmt.Errorf("fetch config error: worker ID should start with fetch_ - '%v'", cfg.WorkerId)
	}

	if len(cfg.APIHost) == 0 {
		return FetchConfig{}, errors.New("fetch config error: empty API host")
	}
	if cfg.APIPort < 1024 {
		return FetchConfig{}, fmt.Errorf("fetch config error: invalid API port %d", cfg.APIPort)
	}

	return cfg, nil
}
