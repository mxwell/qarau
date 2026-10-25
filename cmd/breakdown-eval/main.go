// breakdown-eval runs sentence breakdowns for a (prompt, model) config over a
// fixed eval set and scores the cached results.
//
//	breakdown-eval run   --prompt v2 --model gpt-5.6-luna [--effort low]
//	breakdown-eval score [--baseline v1@gpt-5.6-luna]
//	breakdown-eval view  # side-by-side HTML of the cached runs
//
// Working directory layout (--dir, "eval" by default):
//
//	eval/
//	├── eval_set.20261006.json                  # copied from the admin endpoint output
//	├── runs/eval_set.20261006/v2@gpt-5.6-luna/b00.r1.json
//	└── view/eval_set.20261006.html
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mxwell/qarau/internal/evalset"
)

const (
	defaultDir = "eval"
	runsSubdir = "runs"
)

type WordOutput struct {
	Word            string `json:"word"`
	PartOfSpeech    string `json:"pos"`
	Base            string `json:"base"`
	BaseTranslation string `json:"base_translation"`
	WordTranslation string `json:"word_translation"`
	Comment         string `json:"comment"`
}

type SentenceOutput struct {
	// empty if the model skipped the sentence
	Sentence     string       `json:"sentence"`
	Translations []string     `json:"translations"`
	Breakdown    []WordOutput `json:"breakdown"`
}

// RunRecord is a single (config, batch, repeat) result, cached on disk
type RunRecord struct {
	Config       string           `json:"config"`
	Prompt       uint             `json:"prompt"`
	Model        string           `json:"model"`
	Effort       string           `json:"effort,omitempty"`
	BatchIndex   int              `json:"batch_index"`
	Repeat       int              `json:"repeat"`
	VideoID      int64            `json:"video_id"`
	StartSentSeq int32            `json:"start_sent_seq"`
	Sentences    []string         `json:"sentences"`
	StartedAt    time.Time        `json:"started_at"`
	LatencyMs    int64            `json:"latency_ms"`
	InputTokens  int64            `json:"input_tokens"`
	OutputTokens int64            `json:"output_tokens"`
	Error        string           `json:"error,omitempty"`
	Output       []SentenceOutput `json:"output,omitempty"`
}

func parsePromptVersion(s string) (uint, error) {
	if !strings.HasPrefix(s, "v") {
		return 0, fmt.Errorf("prompt must look like v1, v2, ...: %q", s)
	}
	ver, err := strconv.ParseUint(strings.TrimPrefix(s, "v"), 10, 32)
	if err != nil || ver == 0 {
		return 0, fmt.Errorf("invalid prompt version: %q", s)
	}
	return uint(ver), nil
}

func configName(prompt uint, model, effort string) string {
	name := fmt.Sprintf("v%d@%s", prompt, model)
	if effort != "" {
		name += "@" + effort
	}
	return name
}

func loadSet(dir, name string) (evalset.Set, string, error) {
	var set evalset.Set
	if name == "" {
		matches, err := filepath.Glob(filepath.Join(dir, "eval_set.*.json"))
		if err != nil {
			return set, "", err
		}
		if len(matches) == 0 {
			return set, "", fmt.Errorf("no eval_set.*.json in %s, copy the eval set there", dir)
		}
		sort.Strings(matches)
		name = filepath.Base(matches[len(matches)-1])
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return set, "", fmt.Errorf("eval set read fail: %w", err)
	}
	if err = json.Unmarshal(data, &set); err != nil {
		return set, "", fmt.Errorf("eval set parse fail: %w", err)
	}
	if len(set.Batches) == 0 {
		return set, "", errors.New("no batches in eval set")
	}
	for i, b := range set.Batches {
		if len(b.Sentences) == 0 {
			return set, "", fmt.Errorf("no sentences in batch %d", i)
		}
	}
	return set, strings.TrimSuffix(name, filepath.Ext(name)), nil
}

func runFileName(batchIndex, repeat int) string {
	return fmt.Sprintf("b%02d.r%d.json", batchIndex, repeat)
}

func readRecord(path string) (RunRecord, error) {
	var rec RunRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	err = json.Unmarshal(data, &rec)
	return rec, err
}

// write via rename, so an interrupted run never leaves a half-written cache entry
func writeRecord(path string, rec RunRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: breakdown-eval run|score|view [flags]; see -h of each subcommand")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runCmd(os.Args[2:])
	case "score":
		err = scoreCmd(os.Args[2:])
	case "view":
		err = viewCmd(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
