package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/mxwell/qarau/internal/config"
	"github.com/mxwell/qarau/internal/llm"
	"golang.org/x/sync/errgroup"
)

type runTask struct {
	batchIndex int
	repeat     int
	path       string
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var (
		dir         = fs.String("dir", defaultDir, "eval working directory")
		setName     = fs.String("set", "", "eval set file name inside --dir (default: latest eval_set.*.json)")
		promptFlag  = fs.String("prompt", "", "prompt version, e.g. v2")
		model       = fs.String("model", "", "model, e.g. gpt-5.6-luna")
		effort      = fs.String("effort", "", "reasoning effort (none|minimal|low|medium|high|xhigh|max), empty for the model default")
		repeats     = fs.Int("repeats", 3, "runs per batch")
		parallel    = fs.Int("parallel", 4, "concurrent LLM requests")
		batchLimit  = fs.Int("batches", -1, "use only the first N batches of the set")
		retryFailed = fs.Bool("retry-failed", false, "redo runs whose cached result is an error")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	promptVersion, err := parsePromptVersion(*promptFlag)
	if err != nil {
		return err
	}
	if *model == "" {
		return errors.New("no --model specified")
	}
	if *repeats < 1 || *parallel < 1 {
		return errors.New("--repeats and --parallel must be positive")
	}

	set, setStem, err := loadSet(*dir, *setName)
	if err != nil {
		return err
	}
	batches := set.Batches
	if *batchLimit > 0 && *batchLimit < len(batches) {
		batches = batches[:*batchLimit]
	}

	cfgName := configName(promptVersion, *model, *effort)
	outDir := filepath.Join(*dir, runsSubdir, setStem, cfgName)
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	tasks := make([]runTask, 0, len(batches)*(*repeats))
	cached := 0
	for bi := range batches {
		for r := 1; r <= *repeats; r++ {
			path := filepath.Join(outDir, runFileName(bi, r))
			if rec, readErr := readRecord(path); readErr == nil {
				if rec.Error == "" || !*retryFailed {
					cached++
					continue
				}
			}
			tasks = append(tasks, runTask{batchIndex: bi, repeat: r, path: path})
		}
	}
	fmt.Printf("%s on %s: %d runs to do, %d cached\n", cfgName, setStem, len(tasks), cached)
	if len(tasks) == 0 {
		return nil
	}

	cfg, err := config.LoadLLM()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	// client logs every prompt at info level, keep only problems
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	client, err := llm.NewWithModel(logger, cfg.LlmApiKey, *model, *effort)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		mu       sync.Mutex
		done     int
		failures int
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(*parallel)
	for _, task := range tasks {
		group.Go(func() error {
			batch := batches[task.batchIndex]
			rec := RunRecord{
				Config:       cfgName,
				Prompt:       promptVersion,
				Model:        *model,
				Effort:       *effort,
				BatchIndex:   task.batchIndex,
				Repeat:       task.repeat,
				VideoID:      batch.VideoID,
				StartSentSeq: batch.StartSentSeq,
				Sentences:    batch.Sentences,
				StartedAt:    time.Now(),
			}
			result, callErr := client.DoSentenceBreakdown(
				groupCtx,
				llm.LangRu,
				batch.VideoTitle,
				batch.ChannelTitle,
				promptVersion,
				batch.Sentences,
			)
			if groupCtx.Err() != nil {
				// interrupted, don't cache
				return groupCtx.Err()
			}
			rec.LatencyMs = time.Since(rec.StartedAt).Milliseconds()
			rec.InputTokens = result.Usage.Input
			rec.OutputTokens = result.Usage.Output
			if callErr != nil {
				rec.Error = callErr.Error()
			} else {
				rec.Output = toSentenceOutputs(result.Sentences)
			}
			if err := writeRecord(task.path, rec); err != nil {
				return fmt.Errorf("run record write fail: %w", err)
			}

			mu.Lock()
			defer mu.Unlock()
			done++
			status := "ok"
			if rec.Error != "" {
				failures++
				status = "ERROR: " + rec.Error
			}
			fmt.Printf(
				"[%d/%d] b%02d r%d %6.1fs in=%d out=%d %s\n",
				done, len(tasks), task.batchIndex, task.repeat,
				float64(rec.LatencyMs)/1000, rec.InputTokens, rec.OutputTokens, status,
			)
			return nil
		})
	}
	if err = group.Wait(); err != nil {
		return err
	}
	fmt.Printf("%s: %d runs done, %d failed\n", cfgName, done, failures)
	return nil
}

func toSentenceOutputs(sentences []llm.SentenceBreakdownGenericV1) []SentenceOutput {
	result := make([]SentenceOutput, 0, len(sentences))
	for _, sent := range sentences {
		words := make([]WordOutput, 0, len(sent.Breakdown))
		for _, w := range sent.Breakdown {
			words = append(words, WordOutput{
				Word:            w.Word,
				PartOfSpeech:    w.PartOfSpeech,
				Base:            w.Base,
				BaseTranslation: w.BaseTranslation,
				WordTranslation: w.WordTranslation,
				Comment:         w.Comment,
			})
		}
		result = append(result, SentenceOutput{
			Sentence:     sent.Sentence,
			Translations: sent.Translations,
			Breakdown:    words,
		})
	}
	return result
}
