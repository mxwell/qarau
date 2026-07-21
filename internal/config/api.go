package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type APIConfig struct {
	GRPCPort    uint16
	MTlsCaCert  string
	MTlsCert    string
	MTlsKey     string
	RESTPort    uint16
	GracePeriod time.Duration
	DBUrl       string
	YTApiKey    string
	LogDir      string
	LogLevel    slog.Level
}

func LoadAPI() (APIConfig, error) {
	v := viper.New()

	v.SetConfigFile(".env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("GRPC_PORT", 7991)
	v.SetDefault("MTLS_CA_CERT", "certs/ca.crt")
	v.SetDefault("MTLS_CERT", "certs/api.crt")
	v.SetDefault("MTLS_KEY", "certs/api.key")
	v.SetDefault("REST_PORT", 7992)
	v.SetDefault("GRACE_PERIOD", "3s")
	v.SetDefault("LOG_DIR", "logs")
	v.SetDefault("LOG_LEVEL", "INFO")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return APIConfig{}, fmt.Errorf("api config error: %w", err)
	}

	logLevel, err := parseLogLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return APIConfig{}, fmt.Errorf("api config error: %w", err)
	}

	cfg := APIConfig{
		GRPCPort:    v.GetUint16("GRPC_PORT"),
		MTlsCaCert:  v.GetString("MTLS_CA_CERT"),
		MTlsCert:    v.GetString("MTLS_CERT"),
		MTlsKey:     v.GetString("MTLS_KEY"),
		RESTPort:    v.GetUint16("REST_PORT"),
		GracePeriod: v.GetDuration("GRACE_PERIOD"),
		DBUrl:       v.GetString("DB_URL"),
		YTApiKey:    v.GetString("YT_API_KEY"),
		LogDir:      v.GetString("LOG_DIR"),
		LogLevel:    logLevel,
	}

	if cfg.GRPCPort < 1024 {
		return cfg, fmt.Errorf("api config error: invalid GRPC port %d", cfg.GRPCPort)
	}
	if cfg.MTlsCaCert == "" || cfg.MTlsCert == "" || cfg.MTlsKey == "" {
		return cfg, fmt.Errorf("api config error: some cert paths are missing: '%s', '%s', '%s'", cfg.MTlsCaCert, cfg.MTlsCert, cfg.MTlsKey)
	}
	if cfg.RESTPort < 1024 {
		return cfg, fmt.Errorf("api config error: invalid REST port %d", cfg.RESTPort)
	}
	if cfg.GracePeriod == 0 {
		return cfg, errors.New("api config error: invalid grace period")
	}
	if !strings.HasPrefix(cfg.DBUrl, "postgres://") {
		return cfg, fmt.Errorf("api config error: invalid DB URL %v", cfg.DBUrl)
	}
	if cfg.YTApiKey == "" {
		return cfg, errors.New("api config error: invalid YouTube API key")
	}
	if cfg.LogDir == "" {
		return cfg, fmt.Errorf("api config error: empty log dir")
	}
	return cfg, nil
}
