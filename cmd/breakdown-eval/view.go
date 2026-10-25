package main

import (
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"

	"github.com/mxwell/qarau/internal/evalset"
)

//go:embed view.html.tmpl
var viewTemplateText string

var viewTemplate = template.Must(template.New("view").Parse(viewTemplateText))

type viewBatch struct {
	VideoTitle    string   `json:"video_title"`
	ChannelTitle  string   `json:"channel_title"`
	OnlineVideoID string   `json:"online_video_id"`
	StartSentSeq  int32    `json:"start_sent_seq"`
	Sentences     []string `json:"sentences"`
	Langs         []lang   `json:"langs"`
}

type viewSentence struct {
	SentenceOutput
	Check sentenceCheck `json:"check"`
}

type viewRun struct {
	Error        string         `json:"error,omitempty"`
	LatencyMs    int64          `json:"latency_ms"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	Sentences    []viewSentence `json:"sentences"`
}

type viewData struct {
	Set     string      `json:"set"`
	Batches []viewBatch `json:"batches"`
	Configs []string    `json:"configs"`
	Repeats []int       `json:"repeats"`
	// config -> batch index -> repeat -> run
	Runs map[string]map[int]map[int]viewRun `json:"runs"`
}

func viewCmd(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	var (
		dir     = fs.String("dir", defaultDir, "eval working directory")
		setName = fs.String("set", "", "eval set file name inside --dir (default: latest eval_set.*.json)")
		out     = fs.String("out", "", "output HTML path (default: <dir>/view/<set>.html)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	set, setStem, err := loadSet(*dir, *setName)
	if err != nil {
		return err
	}
	data, err := buildViewData(set, setStem, filepath.Join(*dir, runsSubdir, setStem))
	if err != nil {
		return err
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(*dir, "view", setStem+".html")
	}
	if err = os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = viewTemplate.Execute(f, data); err != nil {
		return fmt.Errorf("view render fail: %w", err)
	}
	fmt.Printf("%d configs, %d batches -> %s\n", len(data.Configs), len(data.Batches), outPath)
	return nil
}

func buildViewData(set evalset.Set, setStem, runsDir string) (viewData, error) {
	data := viewData{
		Set:  setStem,
		Runs: map[string]map[int]map[int]viewRun{},
	}
	for _, b := range set.Batches {
		langs := make([]lang, len(b.Sentences))
		for i, s := range b.Sentences {
			langs[i] = classifyLang(tokenize(s))
		}
		data.Batches = append(data.Batches, viewBatch{
			VideoTitle:    b.VideoTitle,
			ChannelTitle:  b.ChannelTitle,
			OnlineVideoID: b.OnlineVideoID,
			StartSentSeq:  b.StartSentSeq,
			Sentences:     b.Sentences,
			Langs:         langs,
		})
	}

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return data, fmt.Errorf("no runs for %s: %w", setStem, err)
	}
	repeats := map[int]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		paths, err := filepath.Glob(filepath.Join(runsDir, e.Name(), "b*.r*.json"))
		if err != nil {
			return data, err
		}
		if len(paths) == 0 {
			continue
		}
		cfgRuns := map[int]map[int]viewRun{}
		for _, path := range paths {
			rec, err := readRecord(path)
			if err != nil {
				return data, fmt.Errorf("%s: %w", path, err)
			}
			if rec.BatchIndex < 0 || rec.BatchIndex >= len(set.Batches) {
				return data, fmt.Errorf("%s: batch index %d is out of the set", path, rec.BatchIndex)
			}
			if cfgRuns[rec.BatchIndex] == nil {
				cfgRuns[rec.BatchIndex] = map[int]viewRun{}
			}
			cfgRuns[rec.BatchIndex][rec.Repeat] = toViewRun(rec)
			repeats[rec.Repeat] = true
		}
		data.Configs = append(data.Configs, e.Name())
		data.Runs[e.Name()] = cfgRuns
	}
	if len(data.Configs) == 0 {
		return data, fmt.Errorf("no runs found in %s", runsDir)
	}
	sort.Strings(data.Configs)
	for r := range repeats {
		data.Repeats = append(data.Repeats, r)
	}
	sort.Ints(data.Repeats)
	return data, nil
}

func toViewRun(rec RunRecord) viewRun {
	run := viewRun{
		Error:        rec.Error,
		LatencyMs:    rec.LatencyMs,
		InputTokens:  rec.InputTokens,
		OutputTokens: rec.OutputTokens,
	}
	if rec.Error != "" {
		return run
	}
	run.Sentences = make([]viewSentence, len(rec.Sentences))
	for i, input := range rec.Sentences {
		out := recordOutput(rec, i)
		run.Sentences[i] = viewSentence{
			SentenceOutput: out,
			Check:          checkSentence(input, out),
		}
	}
	return run
}
