package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type LLMConfig struct {
	LlmApiKey   string
	LlmInPrice  string
	LlmOutPrice string
}

func LoadLLM() (LLMConfig, error) {
	v := viper.New()

	v.SetConfigFile(".llm.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return LLMConfig{}, fmt.Errorf("llm config error: %w", err)
	}

	cfg := LLMConfig{
		LlmApiKey:   v.GetString("LLM_API_KEY"),
		LlmInPrice:  v.GetString("LLM_INPUT_PRICE"),
		LlmOutPrice: v.GetString("LLM_OUTPUT_PRICE"),
	}

	if !strings.HasPrefix(cfg.LlmApiKey, "sk-") {
		return cfg, errors.New("llm config error: invalid LLM API key")
	}
	if cfg.LlmInPrice == "" || cfg.LlmOutPrice == "" {
		return cfg, fmt.Errorf("llm config error: some llm fields are missing: '%s', '%s'", cfg.LlmInPrice, cfg.LlmOutPrice)
	}

	return cfg, nil
}
