package vad

import (
	"log/slog"
	"os"
	"testing"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

func getLog() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})
	logger := slog.New(handler)
	return logger
}

func Test_RestoreWordTimestamps_WordsBeforeRanges(t *testing.T) {
	ranges := []SegmentRange{
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
		{3000 * SampleRateKhz, 3500 * SampleRateKhz, 2000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    0,
			EndMs:      100,
			Confidence: 100,
		},
		{
			Word:       "w2",
			StartMs:    200,
			EndMs:      300,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 0 {
		t.Fatalf("expected empty result: %d", len(restored))
	}
}

func Test_RestoreWordTimestamps_WordsAfterRanges(t *testing.T) {
	ranges := []SegmentRange{
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
		{3000 * SampleRateKhz, 3500 * SampleRateKhz, 2000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    4000,
			EndMs:      4100,
			Confidence: 100,
		},
		{
			Word:       "w2",
			StartMs:    4200,
			EndMs:      4300,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 0 {
		t.Fatalf("expected empty result: %d", len(restored))
	}
}

func Test_RestoreWordTimestamps_OneMatch(t *testing.T) {
	ranges := []SegmentRange{
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
			Speaker:    11,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 1 {
		t.Fatalf("expected result with 1 word: %d", len(restored))
	}
	rw := restored[0]
	if rw.StartMs != 2100 {
		t.Fatalf("result word start is %d instead of %d", rw.StartMs, 2100)
	}
	if rw.EndMs != 2200 {
		t.Fatalf("result word end is %d instead of %d", rw.EndMs, 2200)
	}
	if rw.Speaker != 11 {
		t.Fatalf("result word speaker id is %d instead of %d", rw.Speaker, 11)
	}
}

func Test_RestoreWordTimestamps_MatchOneFullOnePartial(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
		},
		{
			Word:       "w2",
			StartMs:    1300,
			EndMs:      1600,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 2 {
		t.Fatalf("expected result with 2 words: %d", len(restored))
	}
	rw1 := restored[0]
	if rw1.StartMs != 2100 {
		t.Fatalf("result word 1 start is %d instead of %d", rw1.StartMs, 2100)
	}
	if rw1.EndMs != 2200 {
		t.Fatalf("result word 1 end is %d instead of %d", rw1.EndMs, 2200)
	}
	rw2 := restored[1]
	if rw2.StartMs != 2300 {
		t.Fatalf("result word 2 start is %d instead of %d", rw2.StartMs, 2300)
	}
	if rw2.EndMs != 2500 {
		t.Fatalf("result word 2 end is %d instead of %d", rw2.EndMs, 2500)
	}
}

func Test_RestoreWordTimestamps_NoWords(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 0 {
		t.Fatalf("expected result with 0 words: %d", len(restored))
	}
}

func Test_RestoreWordTimestamps_NoRanges(t *testing.T) {
	ranges := []SegmentRange{}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
		},
		{
			Word:       "w2",
			StartMs:    1300,
			EndMs:      1600,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 0 {
		t.Fatalf("expected result with 0 words: %d", len(restored))
	}
}

func Test_RestoreWordTimestamps_MatchInDiffRanges(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1500 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w100",
			StartMs:    100,
			EndMs:      200,
			Confidence: 100,
		},
		{
			Word:       "w101",
			StartMs:    1500,
			EndMs:      2000,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 2 {
		t.Fatalf("expected result with 2 words: %d", len(restored))
	}
	rw1 := restored[0]
	if rw1.StartMs != 300 {
		t.Fatalf("result word start is %d instead of %d", rw1.StartMs, 300)
	}
	if rw1.EndMs != 400 {
		t.Fatalf("result word end is %d instead of %d", rw1.EndMs, 400)
	}
}

func Test_RestoreWordTimestamps_MatchOneWordStart(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1500 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w100",
			StartMs:    100,
			EndMs:      1200,
			Confidence: 100,
		},
		{
			Word:       "w101",
			StartMs:    1900,
			EndMs:      2000,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 2 {
		t.Fatalf("expected result with 2 words: %d", len(restored))
	}
	rw1 := restored[0]
	if rw1.StartMs != 300 {
		t.Fatalf("result word 1 start is %d instead of %d", rw1.StartMs, 300)
	}
	if rw1.EndMs != 1200 {
		t.Fatalf("result word 1 end is %d instead of %d", rw1.EndMs, 1000)
	}
	rw2 := restored[1]
	if rw2.StartMs != 2400 {
		t.Fatalf("result word 2 start is %d instead of %d", rw2.StartMs, 2400)
	}
	if rw2.EndMs != 2500 {
		t.Fatalf("result word 2 end is %d instead of %d", rw2.EndMs, 2500)
	}
}

func Test_RestoreWordTimestamps_MatchTwoPartial(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []*qarauv1.Word{
		{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      10000, // needs fixing during restoration
			Confidence: 100,
		},
		{
			Word:       "w2",
			StartMs:    1200,
			EndMs:      1300,
			Confidence: 100,
		},
		{
			Word:       "w3",
			StartMs:    900, // needs fixing during restoration
			EndMs:      1400,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 3 {
		t.Fatalf("expected result with 3 words: %d", len(restored))
	}
	rw1 := restored[0]
	if rw1.StartMs != 2100 {
		t.Fatalf("result word 1 start is %d instead of %d", rw1.StartMs, 2100)
	}
	if rw1.EndMs != 2200 {
		t.Fatalf("result word 1 end is %d instead of %d", rw1.EndMs, 2200)
	}
	rw2 := restored[1]
	if rw2.StartMs != 2200 {
		t.Fatalf("result word 2 start is %d instead of %d", rw2.StartMs, 2200)
	}
	if rw2.EndMs != 2300 {
		t.Fatalf("result word 2 end is %d instead of %d", rw2.EndMs, 2300)
	}
	rw3 := restored[2]
	if rw3.StartMs != 2300 {
		t.Fatalf("result word 3 start is %d instead of %d", rw3.StartMs, 2300)
	}
	if rw3.EndMs != 2400 {
		t.Fatalf("result word 3 end is %d instead of %d", rw3.EndMs, 2400)
	}
}
