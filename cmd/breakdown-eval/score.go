package main

import (
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mxwell/qarau/internal/evalset"
)

const (
	bootstrapRounds = 2000
	bootstrapSeed   = 20261006
	// fewer batches than this make the bootstrap CI unreliable
	minBatchesForCI = 15
)

type metricDef struct {
	key      string
	title    string
	scale    float64
	decimals int
}

var metrics = []metricDef{
	{"calls_ok", "calls ok, %", 100, 1},
	{"missing", "missing sentences, %", 100, 1},
	{"echo_ok", "sentence echoed exactly, %", 100, 1},
	{"coverage", "input words covered, %", 100, 1},
	{"extra", "extra words in breakdown, %", 100, 1},
	{"granularity", "items per word", 1, 2},
	{"low_gran", "sentences with items < 2/3 words, %", 100, 1},
	{"gran_first", "items per word, first third", 1, 2},
	{"gran_last", "items per word, last third", 1, 2},
	{"comment_present", "affixed items with comment, %", 100, 1},
	{"comment_affix", "affixed items naming a -suffix, %", 100, 1},
	{"two_tr", "multi-word sentences with 2+ translations, %", 100, 1},
	{"in_tok", "input tokens per sentence", 1, 0},
	{"out_tok", "output tokens per sentence", 1, 0},
	{"cost", "cost per 100 sentences, $", 1, 4},
}

type counter struct {
	num float64
	den float64
}

func (c counter) value() (float64, bool) {
	if c.den == 0 {
		return 0, false
	}
	return c.num / c.den, true
}

type stats map[string]counter

func (s stats) add(key string, num, den float64) {
	c := s[key]
	c.num += num
	c.den += den
	s[key] = c
}

func (s stats) merge(o stats) {
	for k, c := range o {
		s.add(k, c.num, c.den)
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Per-record counters. Ratios are aggregated as sum(num)/sum(den), so
// longer sentences and batches weigh proportionally more.
func scoreRecord(rec RunRecord, prices map[string]Price) stats {
	s := stats{}
	n := float64(len(rec.Sentences))
	s.add("calls_ok", b2f(rec.Error == ""), 1)
	s.add("in_tok", float64(rec.InputTokens), n)
	s.add("out_tok", float64(rec.OutputTokens), n)
	if p, ok := prices[rec.Model]; ok {
		cost := (p.Input*float64(rec.InputTokens) + p.Output*float64(rec.OutputTokens)) / 1e6
		s.add("cost", 100*cost, n)
	}
	if rec.Error != "" {
		return s
	}

	third := len(rec.Sentences) / 3
	for i, input := range rec.Sentences {
		c := checkSentence(input, recordOutput(rec, i))
		s.add("missing", b2f(c.Missing), 1)
		if c.Missing {
			continue
		}
		s.add("echo_ok", b2f(c.EchoOK), 1)
		if c.Lang != langKz {
			continue
		}
		s.add("coverage", float64(c.Matched), float64(c.Words))
		s.add("extra", float64(c.OutWords-c.Matched), float64(c.OutWords))

		if c.multiWordKz() {
			items := float64(c.Items)
			words := float64(c.Words)
			s.add("granularity", items, words)
			s.add("low_gran", b2f(c.LowGran), 1)
			if i < third {
				s.add("gran_first", items, words)
			} else if i >= len(rec.Sentences)-third {
				s.add("gran_last", items, words)
			}
			s.add("two_tr", b2f(!c.FewTranslations), 1)
		}

		for _, ic := range c.ItemChecks {
			if !ic.Affixed {
				continue
			}
			s.add("comment_present", b2f(ic.HasComment), 1)
			s.add("comment_affix", b2f(ic.NamesAffix), 1)
		}
	}
	return s
}

func recordOutput(rec RunRecord, i int) SentenceOutput {
	if i < len(rec.Output) {
		return rec.Output[i]
	}
	return SentenceOutput{}
}

type configResult struct {
	name string
	// batch index -> repeat -> stats
	runs       map[int]map[int]stats
	latencies  []float64
	records    int
	failedRuns int
}

func (c *configResult) total() stats {
	result := stats{}
	for _, reps := range c.runs {
		for _, s := range reps {
			result.merge(s)
		}
	}
	return result
}

func (c *configResult) perRepeat() map[int]stats {
	result := map[int]stats{}
	for _, reps := range c.runs {
		for r, s := range reps {
			if result[r] == nil {
				result[r] = stats{}
			}
			result[r].merge(s)
		}
	}
	return result
}

func (c *configResult) perBatch() map[int]stats {
	result := map[int]stats{}
	for bi, reps := range c.runs {
		result[bi] = stats{}
		for _, s := range reps {
			result[bi].merge(s)
		}
	}
	return result
}

func loadConfig(dir, name string, prices map[string]Price) (*configResult, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "b*.r*.json"))
	if err != nil {
		return nil, err
	}
	result := &configResult{name: name, runs: map[int]map[int]stats{}}
	for _, path := range paths {
		rec, err := readRecord(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if result.runs[rec.BatchIndex] == nil {
			result.runs[rec.BatchIndex] = map[int]stats{}
		}
		result.runs[rec.BatchIndex][rec.Repeat] = scoreRecord(rec, prices)
		result.latencies = append(result.latencies, float64(rec.LatencyMs)/1000)
		result.records++
		if rec.Error != "" {
			result.failedRuns++
		}
	}
	return result, nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func formatValue(m metricDef, v float64) string {
	return fmt.Sprintf("%.*f", m.decimals, v*m.scale)
}

// value plus the min..max range across repeats, if there are several
func formatCell(m metricDef, c *configResult) string {
	v, ok := c.total()[m.key].value()
	if !ok {
		return "-"
	}
	cell := formatValue(m, v)
	reps := c.perRepeat()
	if len(reps) < 2 {
		return cell
	}
	lo, hi := 0.0, 0.0
	first := true
	for _, s := range reps {
		rv, ok := s[m.key].value()
		if !ok {
			continue
		}
		if first || rv < lo {
			lo = rv
		}
		if first || rv > hi {
			hi = rv
		}
		first = false
	}
	return fmt.Sprintf("%s [%s..%s]", cell, formatValue(m, lo), formatValue(m, hi))
}

func ratioOver(batches []stats, key string) (float64, bool) {
	total := counter{}
	for _, s := range batches {
		total.num += s[key].num
		total.den += s[key].den
	}
	return total.value()
}

// Paired bootstrap over whole batches: a batch is resampled as a unit, keeping
// its sentences together, since sentences of one batch share context and are not
// independent. Returns the delta (candidate - baseline) and its 95% CI.
func bootstrapDelta(base, cand map[int]stats, common []int, key string) (float64, float64, float64, bool) {
	baseAll := make([]stats, 0, len(common))
	candAll := make([]stats, 0, len(common))
	for _, bi := range common {
		baseAll = append(baseAll, base[bi])
		candAll = append(candAll, cand[bi])
	}
	bv, ok1 := ratioOver(baseAll, key)
	cv, ok2 := ratioOver(candAll, key)
	if !ok1 || !ok2 {
		return 0, 0, 0, false
	}

	rng := rand.New(rand.NewPCG(bootstrapSeed, uint64(len(common))))
	deltas := make([]float64, 0, bootstrapRounds)
	baseSample := make([]stats, len(common))
	candSample := make([]stats, len(common))
	for range bootstrapRounds {
		for i := range common {
			j := rng.IntN(len(common))
			baseSample[i] = baseAll[j]
			candSample[i] = candAll[j]
		}
		b, ok1 := ratioOver(baseSample, key)
		c, ok2 := ratioOver(candSample, key)
		if ok1 && ok2 {
			deltas = append(deltas, c-b)
		}
	}
	if len(deltas) == 0 {
		return cv - bv, 0, 0, false
	}
	sort.Float64s(deltas)
	return cv - bv, percentile(deltas, 0.025), percentile(deltas, 0.975), true
}

func scoreCmd(args []string) error {
	fs := flag.NewFlagSet("score", flag.ExitOnError)
	var (
		dir      = fs.String("dir", defaultDir, "eval working directory")
		setName  = fs.String("set", "", "eval set file name inside --dir (default: latest eval_set.*.json)")
		baseline = fs.String("baseline", "", "config to compare against, e.g. v1@gpt-5.6-luna")
		only     = fs.String("configs", "", "comma-separated configs to show (default: all)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	set, setStem, err := loadSet(*dir, *setName)
	if err != nil {
		return err
	}
	prices, err := loadPrices(*dir)
	if err != nil {
		return err
	}

	runsDir := filepath.Join(*dir, runsSubdir, setStem)
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return fmt.Errorf("no runs for %s: %w", setStem, err)
	}
	var wanted []string
	if *only != "" {
		wanted = strings.Split(*only, ",")
		if *baseline != "" && !slices.Contains(wanted, *baseline) {
			wanted = append(wanted, *baseline)
		}
	}
	var configs []*configResult
	for _, e := range entries {
		if !e.IsDir() || (wanted != nil && !slices.Contains(wanted, e.Name())) {
			continue
		}
		c, err := loadConfig(filepath.Join(runsDir, e.Name()), e.Name(), prices)
		if err != nil {
			return err
		}
		if c.records > 0 {
			configs = append(configs, c)
		}
	}
	if len(configs) == 0 {
		return errors.New("no runs found")
	}
	// baseline first, the rest by name
	sort.Slice(configs, func(i, j int) bool {
		if (configs[i].name == *baseline) != (configs[j].name == *baseline) {
			return configs[i].name == *baseline
		}
		return configs[i].name < configs[j].name
	})

	printSetSummary(set, setStem)
	printMetrics(configs)

	if *baseline == "" {
		fmt.Println("\npass --baseline <config> for deltas with 95% CI")
		return nil
	}
	if configs[0].name != *baseline {
		return fmt.Errorf("baseline %s has no runs", *baseline)
	}
	printDeltas(configs[0], configs[1:])
	return nil
}

func printSetSummary(set evalset.Set, setStem string) {
	sentences, kz, ru, other, single := 0, 0, 0, 0, 0
	for _, b := range set.Batches {
		for _, s := range b.Sentences {
			sentences++
			tokens := tokenize(s)
			switch classifyLang(tokens) {
			case langKz:
				kz++
				if len(tokens) < 2 {
					single++
				}
			case langRu:
				ru++
			default:
				other++
			}
		}
	}
	fmt.Printf(
		"set %s: %d batches, %d sentences (kz %d, of them single-word %d; ru %d; other %d)\n",
		setStem, len(set.Batches), sentences, kz, single, ru, other,
	)
	fmt.Println("word-level metrics use kz sentences only; granularity and translations skip single-word ones")
	fmt.Println("cells: value over all repeats [min..max across repeats]")
	fmt.Println()
}

func printMetrics(configs []*configResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	header := []string{"metric"}
	for _, c := range configs {
		header = append(header, c.name)
	}
	fmt.Fprintln(w, strings.Join(header, "\t"))

	runsRow := []string{"runs (failed)"}
	for _, c := range configs {
		runsRow = append(runsRow, fmt.Sprintf("%d (%d), %d batches", c.records, c.failedRuns, len(c.runs)))
	}
	fmt.Fprintln(w, strings.Join(runsRow, "\t"))

	for _, m := range metrics {
		row := []string{m.title}
		for _, c := range configs {
			row = append(row, formatCell(m, c))
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	latRow := []string{"latency p50 / p95, s"}
	for _, c := range configs {
		lat := slices.Clone(c.latencies)
		sort.Float64s(lat)
		latRow = append(latRow, fmt.Sprintf("%.1f / %.1f", percentile(lat, 0.5), percentile(lat, 0.95)))
	}
	fmt.Fprintln(w, strings.Join(latRow, "\t"))
	w.Flush()
}

func printDeltas(base *configResult, others []*configResult) {
	if len(others) == 0 {
		return
	}
	basePerBatch := base.perBatch()
	fmt.Printf("\ndeltas vs %s, 95%% CI from paired bootstrap over batches; * = CI excludes 0\n", base.name)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	header := []string{"metric"}
	perBatch := make([]map[int]stats, len(others))
	common := make([][]int, len(others))
	for i, c := range others {
		perBatch[i] = c.perBatch()
		for bi := range perBatch[i] {
			if _, ok := basePerBatch[bi]; ok {
				common[i] = append(common[i], bi)
			}
		}
		sort.Ints(common[i])
		header = append(header, fmt.Sprintf("%s (%d batches)", c.name, len(common[i])))
		if len(common[i]) < minBatchesForCI {
			fmt.Printf("warning: %s shares only %d batches with baseline, CI is unreliable below %d\n",
				c.name, len(common[i]), minBatchesForCI)
		}
	}
	fmt.Fprintln(w, strings.Join(header, "\t"))

	for _, m := range metrics {
		row := []string{m.title}
		for i := range others {
			delta, lo, hi, ok := bootstrapDelta(basePerBatch, perBatch[i], common[i], m.key)
			if !ok {
				row = append(row, "-")
				continue
			}
			mark := ""
			if lo > 0 || hi < 0 {
				mark = " *"
			}
			row = append(row, fmt.Sprintf(
				"%+.*f [%+.*f, %+.*f]%s",
				m.decimals, delta*m.scale,
				m.decimals, lo*m.scale,
				m.decimals, hi*m.scale,
				mark,
			))
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}
