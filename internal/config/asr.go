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
	APIHost      string
	APIPort      uint16
	MTlsCaCert   string
	MTlsCert     string
	MTlsKey      string
	WorkingDir   string
	RemoveFiles  bool
	FfmpegTool   string
	VoskModel    string
	ElevenApiKey string
	PcmFile      string // for testing purposes
	LogDir       string
	LogLevel     slog.Level
}

var (
	ErrOneOfVoskAndElevenLabs = errors.New("ASR config error: exactly one of Vosk model and ElevenLabs API key must be specified")
	ErrApiKeyNeedsPrefix      = errors.New("ASR config error: API key must start with sk_")
)

func LoadASR() (ASRConfig, error) {
	v := viper.New()

	v.SetConfigFile(".asr.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	v.SetDefault("API_HOST", "localhost")
	v.SetDefault("API_PORT", 7991)
	v.SetDefault("MTLS_CA_CERT", "certs/ca.crt")
	v.SetDefault("MTLS_CERT", "certs/asr.crt")
	v.SetDefault("MTLS_KEY", "certs/asr.key")
	v.SetDefault("REMOVE_FILES", 1)
	v.SetDefault("LOG_DIR", "logs")
	v.SetDefault("LOG_LEVEL", "INFO")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ASRConfig{}, fmt.Errorf("ASR config error: %w", err)
	}

	logLevel, err := parseLogLevel(v.GetString("LOG_LEVEL"))
	if err != nil {
		return ASRConfig{}, fmt.Errorf("ASR config error: %w", err)
	}

	cfg := ASRConfig{
		APIHost:      v.GetString("API_HOST"),
		APIPort:      v.GetUint16("API_PORT"),
		MTlsCaCert:   v.GetString("MTLS_CA_CERT"),
		MTlsCert:     v.GetString("MTLS_CERT"),
		MTlsKey:      v.GetString("MTLS_KEY"),
		WorkingDir:   v.GetString("WORKING_DIR"),
		RemoveFiles:  v.GetUint8("REMOVE_FILES") > 0,
		FfmpegTool:   v.GetString("FFMPEG"),
		VoskModel:    v.GetString("VOSK_MODEL"),
		ElevenApiKey: v.GetString("ELEVEN_API_KEY"),
		PcmFile:      v.GetString("PCM_FILE"),
		LogDir:       v.GetString("LOG_DIR"),
		LogLevel:     logLevel,
	}

	if len(cfg.APIHost) == 0 {
		return ASRConfig{}, errors.New("ASR config error: empty API host")
	}
	if cfg.APIPort < 1024 {
		return ASRConfig{}, fmt.Errorf("ASR config error: invalid API port %d", cfg.APIPort)
	}
	if cfg.MTlsCaCert == "" || cfg.MTlsCert == "" || cfg.MTlsKey == "" {
		return ASRConfig{}, fmt.Errorf("ASR config error: some cert paths are missing: '%s', '%s', '%s'", cfg.MTlsCaCert, cfg.MTlsCert, cfg.MTlsKey)
	}
	if !filepath.IsAbs(cfg.WorkingDir) {
		return ASRConfig{}, fmt.Errorf("ASR config error: invalid working dir '%v'", cfg.WorkingDir)
	}
	if cfg.LogDir == "" {
		return ASRConfig{}, fmt.Errorf("ASR config error: empty log dir")
	}
	if !strings.HasPrefix(cfg.FfmpegTool, "/") {
		return ASRConfig{}, fmt.Errorf("ASR config error: invalid ffmpeg path '%v'", cfg.FfmpegTool)
	}
	if cfg.VoskModel != "" {
		if !strings.HasPrefix(cfg.VoskModel, "/") {
			return ASRConfig{}, fmt.Errorf("ASR config error: invalid Vosk model path '%v'", cfg.VoskModel)
		}
		if cfg.ElevenApiKey != "" {
			return ASRConfig{}, ErrOneOfVoskAndElevenLabs
		}
	} else if cfg.ElevenApiKey != "" {
		if !strings.HasPrefix(cfg.ElevenApiKey, "sk_") {
			return ASRConfig{}, ErrApiKeyNeedsPrefix
		}
	} else {
		return ASRConfig{}, ErrOneOfVoskAndElevenLabs
	}

	return cfg, nil
}
