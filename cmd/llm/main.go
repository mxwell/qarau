package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/llm"
	"github.com/mxwell/qarau/internal/quota"
)

type InputJson struct {
	VideoTitle   string   `json:"video_title"`
	ChannelTitle string   `json:"channel_title"`
	Sentences    []string `json:"sentences"`
}

type SentenceOutput struct {
	Translation llm.PlainTranslations `json:"translation"`
	Breakdown   llm.PlainBreakdown    `json:"breakdown"`
}

type OutputJson struct {
	Sentences []SentenceOutput `json:"sentences"`
}

func printStreamedSentence(sent llm.StreamedSentence) {
	translation := ""
	if len(sent.Breakdown.Translations) > 0 {
		translation = sent.Breakdown.Translations[0]
	}
	fmt.Printf(
		"[+%7.3fs] sentence %d (%d words): %s\n",
		sent.Elapsed.Seconds(),
		sent.SentIndex,
		len(sent.Breakdown.Breakdown),
		translation,
	)
}

func run() error {
	var (
		promptVersion  = flag.Uint("prompt", 1, "prompt version")
		targetLang     = flag.String("lang", "", "target language")
		inputJsonPath  = flag.String("input", "", "input JSON file path")
		sentenceLimit  = flag.Int("sentences", -1, "how many sentences to use from input")
		outputJsonPath = flag.String("output", "", "output JSON file path")
		streamMode     = flag.Bool("stream", false, "stream the response, printing sentences as they arrive")
		inputJson      InputJson
	)
	flag.Parse()

	if targetLang == nil || len(*targetLang) == 0 {
		return errors.New("no lang specified")
	}
	if *targetLang != llm.LangRu {
		return fmt.Errorf("lang not supported: %s", *targetLang)
	}

	if inputJsonPath == nil || len(*inputJsonPath) == 0 {
		return errors.New("no input specified")
	}

	if outputJsonPath == nil || len(*outputJsonPath) == 0 {
		return errors.New("no output specified")
	}

	inputBytes, err := os.ReadFile(*inputJsonPath)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(inputBytes, &inputJson); err != nil {
		return err
	}
	if inputJson.VideoTitle == "" {
		return errors.New("empty video title")
	}
	if inputJson.ChannelTitle == "" {
		return errors.New("empty channel title")
	}
	if len(inputJson.Sentences) == 0 {
		return errors.New("no sentences in input")
	}

	cfg, err := config.LoadLLM()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logHandler := slog.NewJSONHandler(
		os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelInfo},
	)
	logger := slog.New(logHandler)

	client, err := llm.New(logger, cfg.LlmApiKey)
	if err != nil {
		return err
	}

	llmQuotaCtl, err := quota.NewLlmQuotaController(
		cfg.LlmInPrice,
		cfg.LlmOutPrice,
		"0",
	)
	if err != nil {
		return err
	}

	sentences := inputJson.Sentences
	if *sentenceLimit == 0 {
		return errors.New("invalid sentence limit")
	} else if *sentenceLimit > 0 {
		sentences = sentences[:*sentenceLimit]
	}

	var result llm.SentenceBreakdownResult
	if *streamMode {
		fmt.Printf("streaming breakdown of %d sentences...\n", len(sentences))
		result, err = client.DoSentenceBreakdownStream(
			context.Background(),
			*targetLang,
			inputJson.VideoTitle,
			inputJson.ChannelTitle,
			*promptVersion,
			sentences,
			printStreamedSentence,
		)
	} else {
		result, err = client.DoSentenceBreakdown(
			context.Background(),
			*targetLang,
			inputJson.VideoTitle,
			inputJson.ChannelTitle,
			*promptVersion,
			sentences,
		)
	}
	if err != nil {
		return fmt.Errorf("sentence breakdown fail: %w", err)
	}
	cost := llmQuotaCtl.CalculateCost(result.Usage)
	logger.Info("calculated llm cost", "usd", fmt.Sprintf("%.3f", cost))

	sentenceOutputs := make([]SentenceOutput, 0, len(result.Sentences))
	for _, sent := range result.Sentences {
		sentenceOutputs = append(sentenceOutputs, SentenceOutput{
			Translation: llm.PlainTranslations{
				Variants: sent.Translations,
			},
			Breakdown: llm.PlainBreakdown{
				Words: llm.FromWordGenericV1(sent.Breakdown),
			},
		})
	}

	outputJsonBytes, err := json.Marshal(OutputJson{
		Sentences: sentenceOutputs,
	})
	if err != nil {
		return err
	}

	err = os.WriteFile(*outputJsonPath, outputJsonBytes, 0o644)
	if err != nil {
		return fmt.Errorf("output write fail: %w", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("run failed: %v\n", err.Error())
		os.Exit(1)
	}
}
