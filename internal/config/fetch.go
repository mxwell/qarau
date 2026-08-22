package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type FetchConfig struct {
	APIHost       string
	APIPort       uint16
	MTlsCaCert    string
	MTlsCert      string
	MTlsKey       string
	Tool          string
	JsRuntimeName string
	JsRuntimePath string
	WorkingDir    string
	LogDir        string
	LogLevel      slog.Level
	SentryDSN     string
}

func (c *FetchConfig) GetJsRuntime() string {
	if c.JsRuntimeName == "" {
		return ""
	}
	return c.JsRuntimeName + ":" + c.JsRuntimePath
}

func LoadFetch() (FetchConfig, error) {
	v := viper.New()

	v.SetConfigFile(".fetch.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	v.SetDefault("API_HOST", "localhost")
	v.SetDefault("API_PORT", 7991)
	v.SetDefault("MTLS_CA_CERT", "certs/ca.crt")
	v.SetDefault("MTLS_CERT", "certs/fetch.crt")
	v.SetDefault("MTLS_KEY", "certs/fetch.key")
	v.SetDefault("LOG_DIR", "logs")
	v.SetDefault("LOG_LEVEL", "INFO")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return FetchConfig{}, fmt.Errorf("fetch config error: %w", err)
	}

	logLevel, err := parseLogLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return FetchConfig{}, fmt.Errorf("fetch config error: %w", err)
	}

	jsRuntimeName := v.GetString("JS_RUNTIME_NAME")
	jsRuntimePath := ""
	if jsRuntimeName != "" {
		jsRuntimePath = v.GetString("JS_RUNTIME_PATH")
		if _, err := os.Stat(jsRuntimePath); err != nil {
			return FetchConfig{}, fmt.Errorf("fetch config error: JS_RUNTIME_NAME is set, but JS_RUNTIME_PATH is invalid: %w", err)
		}
	} else {
		if jsRuntimePath := v.GetString("JS_RUNTIME_PATH"); jsRuntimePath != "" {
			return FetchConfig{}, fmt.Errorf("fetch config error: JS_RUNTIME_PATH is set, but JS_RUNTIME_NAME is empty")
		}
	}

	cfg := FetchConfig{
		APIHost:       v.GetString("API_HOST"),
		APIPort:       v.GetUint16("API_PORT"),
		MTlsCaCert:    v.GetString("MTLS_CA_CERT"),
		MTlsCert:      v.GetString("MTLS_CERT"),
		MTlsKey:       v.GetString("MTLS_KEY"),
		Tool:          v.GetString("TOOL"),
		JsRuntimeName: jsRuntimeName,
		JsRuntimePath: jsRuntimePath,
		WorkingDir:    v.GetString("WORKING_DIR"),
		LogDir:        v.GetString("LOG_DIR"),
		LogLevel:      logLevel,
		SentryDSN:     v.GetString("SENTRY_DSN"),
	}

	if len(cfg.APIHost) == 0 {
		return FetchConfig{}, errors.New("fetch config error: empty API host")
	}
	if cfg.APIPort <= 0 {
		return FetchConfig{}, fmt.Errorf("fetch config error: invalid API port %d", cfg.APIPort)
	}
	if cfg.MTlsCaCert == "" || cfg.MTlsCert == "" || cfg.MTlsKey == "" {
		return FetchConfig{}, fmt.Errorf("fetch config error: some cert paths are missing: '%s', '%s', '%s'", cfg.MTlsCaCert, cfg.MTlsCert, cfg.MTlsKey)
	}
	if len(cfg.Tool) == 0 {
		return FetchConfig{}, errors.New("fetch config error: empty tool")
	}
	if !filepath.IsAbs(cfg.WorkingDir) {
		return FetchConfig{}, fmt.Errorf("fetch config error: invalid working dir '%v'", cfg.WorkingDir)
	}
	if cfg.LogDir == "" {
		return FetchConfig{}, fmt.Errorf("fetch config error: empty log dir")
	}

	return cfg, nil
}
