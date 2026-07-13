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
	WorkerID     string
	APIHost      string
	APIPort      uint16
	WorkingDir   string
	RemoveFiles  bool
	FfmpegTool   string
	VoskModel    string
	ElevenApiKey string
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
		WorkerID:     v.GetString("WORKER_ID"),
		APIHost:      v.GetString("API_HOST"),
		APIPort:      v.GetUint16("API_PORT"),
		WorkingDir:   v.GetString("WORKING_DIR"),
		RemoveFiles:  v.GetUint8("REMOVE_FILES") > 0,
		FfmpegTool:   v.GetString("FFMPEG"),
		VoskModel:    v.GetString("VOSK_MODEL"),
		ElevenApiKey: v.GetString("ELEVEN_API_KEY"),
		LogDir:       v.GetString("LOG_DIR"),
		LogLevel:     logLevel,
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
