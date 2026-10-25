package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mxwell/qarau/internal/pricing"
	"github.com/spf13/viper"
)

type LLMConfig struct {
	LlmApiKey string
	LlmModel  string
	LlmPrice  pricing.Price
}

func LoadLLM() (LLMConfig, error) {
	v := viper.New()

	v.SetConfigFile(".llm.env")
	v.SetEnvPrefix("QARAU")
	v.AutomaticEnv()

	v.SetDefault("LLM_MODEL", "gpt-6-luna")

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return LLMConfig{}, fmt.Errorf("llm config error: %w", err)
	}

	cfg := LLMConfig{
		LlmApiKey: v.GetString("LLM_API_KEY"),
		LlmModel:  v.GetString("LLM_MODEL"),
	}

	if !strings.HasPrefix(cfg.LlmApiKey, "sk-") {
		return cfg, errors.New("llm config error: invalid LLM API key")
	}
	price, err := pricing.Lookup(cfg.LlmModel)
	if err != nil {
		return cfg, fmt.Errorf("llm config error: %w", err)
	}
	cfg.LlmPrice = price

	return cfg, nil
}
