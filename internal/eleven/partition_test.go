package eleven

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mxwell/qarau/internal/audio"
	"github.com/mxwell/qarau/internal/vad"
)

func getLog() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})
	logger := slog.New(handler)
	return logger
}

const (
	oneMinuteFrames = 60 * audio.SampleRateHz
)

func Test_partitionFragments_NoFragments(t *testing.T) {
	out := partitionFragments(getLog(), make([]vad.Fragment, 0), 5*60*audio.SampleRateHz)
	if len(out) != 0 {
		t.Fatalf("expected empty output for empty input: %d", len(out))
	}
}

func Test_partitionFragments_ZeroTarget(t *testing.T) {
	fragments := make([]vad.Fragment, 0)
	min5 := 5 * oneMinuteFrames
	fragments = append(fragments, vad.Fragment{
		Content: make([]byte, min5*audio.BytesPer16bitFrame),
		Ranges: []vad.SegmentRange{
			{RealStart: 0, RealEnd: min5, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	})
	out := partitionFragments(getLog(), fragments, 0)
	if len(out) != 0 {
		t.Fatalf("expected empty output with zero targetFrames: %d", len(out))
	}
}

func Test_partitionFragments_2Parts(t *testing.T) {
	fragments := make([]vad.Fragment, 0)
	min4 := 4 * oneMinuteFrames
	min5 := 5 * oneMinuteFrames
	fragments = append(fragments, vad.Fragment{
		Content: make([]byte, min5*audio.BytesPer16bitFrame),
		Ranges: []vad.SegmentRange{
			{RealStart: 0, RealEnd: min5, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	})
	fragments = append(fragments, vad.Fragment{
		Content: make([]byte, min5*audio.BytesPer16bitFrame),
		Ranges: []vad.SegmentRange{
			{RealStart: min5, RealEnd: 2 * min5, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	})
	out := partitionFragments(getLog(), fragments, min4)
	if len(out) != 2 {
		t.Fatalf("%d parts instead of 2", len(out))
	}
}

func Test_partitionFragments_1PartOutOf2(t *testing.T) {
	fragments := make([]vad.Fragment, 0)
	min5 := 5 * oneMinuteFrames
	min5plus := min5 + 1
	fragments = append(fragments, vad.Fragment{
		Content: make([]byte, min5*audio.BytesPer16bitFrame),
		Ranges: []vad.SegmentRange{
			{RealStart: 0, RealEnd: min5, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	})
	fragments = append(fragments, vad.Fragment{
		Content: make([]byte, min5*audio.BytesPer16bitFrame),
		Ranges: []vad.SegmentRange{
			{RealStart: min5, RealEnd: 2 * min5, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	})
	out := partitionFragments(getLog(), fragments, min5plus)
	if len(out) != 1 {
		t.Fatalf("%d parts instead of 1", len(out))
	}
}

func makeBytes(minutes int, value byte) []byte {
	frames := minutes * oneMinuteFrames
	result := make([]byte, frames*audio.BytesPer16bitFrame)
	for i := range result {
		result[i] = value
	}
	return result
}

func Test_partitionFragments_3Parts(t *testing.T) {
	// target = 10 minutes
	// fragments: [5, 6], [3, 7], [8]

	fragments := make([]vad.Fragment, 0)

	fragLengths := []int{5, 6, 3, 7, 8}

	offset := 0
	for _, length := range fragLengths {
		fragments = append(fragments, vad.Fragment{
			Content: makeBytes(length, byte(length)),
			Ranges: []vad.SegmentRange{
				{RealStart: offset * oneMinuteFrames, RealEnd: (offset + length) * oneMinuteFrames, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		})
		offset += length
	}
	out := partitionFragments(getLog(), fragments, 10*oneMinuteFrames)
	if len(out) != 3 {
		t.Fatalf("%d parts instead of 3", len(out))
	}

	partLengths := []int{2, 2, 1}
	for i, partLength := range partLengths {
		if len(out[i]) != partLength {
			t.Fatalf("%d fragments in part #%d instead of %d", len(out[i]), i, partLength)
		}
	}

	if out[0][0].Content[0] != 5 {
		t.Fatalf("%v instead of 5 at 0,0", out[0][0].Content[0])
	}
	if out[0][1].Content[0] != 6 {
		t.Fatalf("%v instead of 6 at 0,1", out[0][1].Content[0])
	}
	if out[1][0].Content[0] != 3 {
		t.Fatalf("%v instead of 3 at 1,0", out[1][0].Content[0])
	}
	if out[1][1].Content[0] != 7 {
		t.Fatalf("%v instead of 7 at 1,1", out[1][1].Content[0])
	}
	if out[2][0].Content[0] != 8 {
		t.Fatalf("%v instead of 8 at 2,0", out[2][0].Content[0])
	}
}

// makeFragment builds a fragment `minutes` long whose Content is filled with
// `marker`, so a test can assert both its size (LengthInFrames) and its
// identity/order (Content[0]).
func makeFragment(minutes int, marker byte) vad.Fragment {
	return vad.Fragment{
		Content: makeBytes(minutes, marker),
		Ranges: []vad.SegmentRange{
			{RealStart: 0, RealEnd: minutes * oneMinuteFrames, CopyStart: 0},
		},
		RealSpeechEnd: -1,
	}
}

func partFrames(part []vad.Fragment) int {
	total := 0
	for i := range part {
		total += part[i].LengthInFrames()
	}
	return total
}

func Test_partitionFragments_SingleFragment(t *testing.T) {
	fragments := []vad.Fragment{makeFragment(5, 1)}
	// target larger than the fragment: only the end-of-input branch can close it
	out := partitionFragments(getLog(), fragments, 10*oneMinuteFrames)
	if len(out) != 1 || len(out[0]) != 1 {
		t.Fatalf("expected 1 part of 1 fragment, got %d parts", len(out))
	}
	if out[0][0].Content[0] != 1 {
		t.Fatalf("%v instead of 1 at 0,0", out[0][0].Content[0])
	}
}

func Test_partitionFragments_TargetLargerThanTotal(t *testing.T) {
	lengths := []int{2, 3, 4} // sum 9, well below the target
	fragments := make([]vad.Fragment, 0, len(lengths))
	for i, length := range lengths {
		fragments = append(fragments, makeFragment(length, byte(i)))
	}
	out := partitionFragments(getLog(), fragments, 100*oneMinuteFrames)
	if len(out) != 1 {
		t.Fatalf("%d parts instead of 1", len(out))
	}
	if len(out[0]) != 3 {
		t.Fatalf("%d fragments in the single part instead of 3", len(out[0]))
	}
	for i := range out[0] {
		if out[0][i].Content[0] != byte(i) {
			t.Fatalf("fragment %d out of order: marker %d", i, out[0][i].Content[0])
		}
	}
}

func Test_partitionFragments_EachFragmentOwnPart(t *testing.T) {
	lengths := []int{5, 5, 5}
	fragments := make([]vad.Fragment, 0, len(lengths))
	for i, length := range lengths {
		fragments = append(fragments, makeFragment(length, byte(i)))
	}
	// every fragment already exceeds the target, so each closes its own part
	out := partitionFragments(getLog(), fragments, 1*oneMinuteFrames)
	if len(out) != 3 {
		t.Fatalf("%d parts instead of 3", len(out))
	}
	for i := range out {
		if len(out[i]) != 1 {
			t.Fatalf("part #%d holds %d fragments instead of 1", i, len(out[i]))
		}
		if out[i][0].Content[0] != byte(i) {
			t.Fatalf("part #%d out of order: marker %d", i, out[i][0].Content[0])
		}
	}
}

// The output must be a true partition of the input: every fragment appears
// exactly once, in the original order, and no part is empty. Also checks the
// greedy property that a non-final part is never closed before reaching target.
func Test_partitionFragments_IsCompletePartition(t *testing.T) {
	lengths := []int{5, 6, 3, 7, 8, 2, 9}
	fragments := make([]vad.Fragment, 0, len(lengths))
	for i, length := range lengths {
		fragments = append(fragments, makeFragment(length, byte(i)))
	}
	target := 10 * oneMinuteFrames
	out := partitionFragments(getLog(), fragments, target)

	flat := make([]vad.Fragment, 0, len(fragments))
	for _, part := range out {
		if len(part) == 0 {
			t.Fatalf("empty part in output")
		}
		flat = append(flat, part...)
	}
	if len(flat) != len(fragments) {
		t.Fatalf("got %d fragments across parts, want %d", len(flat), len(fragments))
	}
	for i := range flat {
		if flat[i].Content[0] != byte(i) {
			t.Fatalf("fragment %d out of order: marker %d", i, flat[i].Content[0])
		}
	}

	for i := 0; i+1 < len(out); i++ {
		if got := partFrames(out[i]); got < target {
			t.Fatalf("non-final part %d has %d frames, below target %d", i, got, target)
		}
	}
}
