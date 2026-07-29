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

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    0,
			EndMs:      100,
			Confidence: 100,
		},
		qarauv1.Word{
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

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    4000,
			EndMs:      4100,
			Confidence: 100,
		},
		qarauv1.Word{
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

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 1 {
		t.Fatalf("expected result with 1 word: %d", len(restored))
	}
	rw := &restored[0]
	if rw.StartMs != 2100 {
		t.Fatalf("result word start is %d instead of %d", rw.StartMs, 2100)
	}
	if rw.EndMs != 2200 {
		t.Fatalf("result word end is %d instead of %d", rw.EndMs, 2200)
	}
}

func Test_RestoreWordTimestamps_MatchOneOutOfTwo(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
		},
		qarauv1.Word{
			Word:       "w2",
			StartMs:    1300,
			EndMs:      1600,
			Confidence: 100,
		},
	}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 1 {
		t.Fatalf("expected result with 1 word: %d", len(restored))
	}
	rw := &restored[0]
	if rw.StartMs != 2100 {
		t.Fatalf("result word start is %d instead of %d", rw.StartMs, 2100)
	}
	if rw.EndMs != 2200 {
		t.Fatalf("result word end is %d instead of %d", rw.EndMs, 2200)
	}
}

func Test_RestoreWordTimestamps_NoWords(t *testing.T) {
	ranges := []SegmentRange{
		{200 * SampleRateKhz, 1200 * SampleRateKhz, 0 * SampleRateKhz},
		{2000 * SampleRateKhz, 2500 * SampleRateKhz, 1000 * SampleRateKhz},
	}

	words := []qarauv1.Word{}

	restored := RestoreWordTimestamps(getLog(), ranges, words)
	if len(restored) != 0 {
		t.Fatalf("expected result with 0 words: %d", len(restored))
	}
}

func Test_RestoreWordTimestamps_NoRanges(t *testing.T) {
	ranges := []SegmentRange{}

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    1100,
			EndMs:      1200,
			Confidence: 100,
		},
		qarauv1.Word{
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

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w100",
			StartMs:    100,
			EndMs:      200,
			Confidence: 100,
		},
		qarauv1.Word{
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
	rw1 := &restored[0]
	if rw1.StartMs != 300 {
		t.Fatalf("result word start is %d instead of %d", rw1.StartMs, 300)
	}
	if rw1.EndMs != 400 {
		t.Fatalf("result word end is %d instead of %d", rw1.EndMs, 400)
	}
}
