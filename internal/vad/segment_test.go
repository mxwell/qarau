package vad

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/streamer45/silero-vad-go/speech"
)

func digestSlice[T any](s []T, stringer func(T) string) string {
	vals := make([]string, 0)
	n := len(s)
	if n <= 10 {
		for _, item := range s {
			vals = append(vals, stringer(item))
		}
	} else {
		for i := range 3 {
			vals = append(vals, stringer(s[i]))
		}
		vals = append(vals, "...")
		for i := range 3 {
			item := s[n-3+i]
			vals = append(vals, stringer(item))
		}
	}
	return fmt.Sprintf("[%d]{%s}", n, strings.Join(vals, " "))
}

func digestBytes(bytes []byte) string {
	return digestSlice(bytes, func(b byte) string {
		return strconv.Itoa(int(b))
	})
}

func digestSegmentRanges(ranges []SegmentRange) string {
	return digestSlice(ranges, func(r SegmentRange) string {
		return fmt.Sprintf("real %d-%d, copy %d", r.RealStart, r.RealEnd, r.CopyStart)
	})
}

func Test_SegmentAudioByTimestampRanges_NoGap(t *testing.T) {
	audio := []byte{1, 2, 3, 4}
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0,
				SpeechEndAt:   1,
			},
		},
		100,
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	if !slices.Equal(audio, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{0, 2, 0},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func makeSeq(start int, count int) []byte {
	result := make([]byte, count)
	for i := range count {
		result[i] = byte((start + i) % 256)
	}
	return result
}

func Test_SegmentAudioByTimestampRanges_OneSegment(t *testing.T) {
	ms1 := makeSeq(0, 16*2)
	ms2 := makeSeq(32, 16*2)
	ms3 := makeSeq(64, 16*2)
	audio := slices.Concat(ms1, ms2, ms3)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.001,
				SpeechEndAt:   0.002,
			},
		},
		100,
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	if !slices.Equal(audio, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{0, 48, 0},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_OneSegmentWithGaps(t *testing.T) {
	ms1 := makeSeq(0, 16*2)
	ms2 := makeSeq(32, 16*2)
	ms3 := makeSeq(64, 16*2)
	ms4 := makeSeq(96, 16*2)
	ms5 := makeSeq(128, 16*2)
	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.002,
				SpeechEndAt:   0.003,
			},
		},
		1,
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	expectedContent := slices.Concat(ms2, ms3, ms4)
	if !slices.Equal(expectedContent, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{16, 64, 0},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_TwoSegmentsSmallGap(t *testing.T) {
	ms1 := makeSeq(0, 16*2)    // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2) // <- gap
	ms3 := makeSeq(32*2, 16*2) // <- speech
	ms4 := makeSeq(48*2, 16*2) // <- gap
	ms5 := makeSeq(64*2, 16*2) // <- speech
	ms6 := makeSeq(80*2, 16*2) // <- gap
	ms7 := makeSeq(96*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.002,
				SpeechEndAt:   0.003,
			},
			speech.Segment{
				SpeechStartAt: 0.004,
				SpeechEndAt:   0.005,
			},
		},
		1,
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	expectedContent := slices.Concat(ms2, ms3, ms4, ms5, ms6)
	if !slices.Equal(expectedContent, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{16, 48, 0},
		SegmentRange{48, 96, 32},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_TwoSegmentsDoublePadGap(t *testing.T) {
	ms1 := makeSeq(0, 16*2)     // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2)  // <- gap
	ms3 := makeSeq(32*2, 16*2)  // <- speech
	ms4 := makeSeq(48*2, 16*2)  // <- gap - first padding here
	ms5 := makeSeq(64*2, 16*2)  // <- gap - another padding here
	ms6 := makeSeq(80*2, 16*2)  // <- speech
	ms7 := makeSeq(96*2, 16*2)  // <- gap
	ms8 := makeSeq(112*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.002,
				SpeechEndAt:   0.003,
			},
			speech.Segment{
				SpeechStartAt: 0.005,
				SpeechEndAt:   0.006,
			},
		},
		1, // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	expectedContent := slices.Concat(ms2, ms3, ms4, ms5, ms6, ms7)
	if !slices.Equal(expectedContent, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{16, 64, 0},
		SegmentRange{64, 112, 48},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_TwoSegmentsWideGap(t *testing.T) {
	ms1 := makeSeq(0, 16*2)     // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2)  // <- gap
	ms3 := makeSeq(32*2, 16*2)  // <- speech
	ms4 := makeSeq(48*2, 16*2)  // <- gap
	ms5 := makeSeq(64*2, 16*2)  // <- gap, will be cut
	ms6 := makeSeq(80*2, 16*2)  // <- gap
	ms7 := makeSeq(96*2, 16*2)  // <- speech
	ms8 := makeSeq(112*2, 16*2) // <- gap
	ms9 := makeSeq(128*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8, ms9)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.002,
				SpeechEndAt:   0.003,
			},
			speech.Segment{
				SpeechStartAt: 0.006,
				SpeechEndAt:   0.007,
			},
		},
		1, // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
		2000,
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 1 {
		t.Fatalf("%d fragments instead of 1", len(fragments))
	}
	fragment := fragments[0]
	expectedContent := slices.Concat(ms2, ms3, ms4, ms6, ms7, ms8)
	if !slices.Equal(expectedContent, fragment.Content) {
		t.Fatalf(
			"fragment has wrong content: %s instead of %s",
			digestBytes(fragment.Content),
			digestBytes(audio),
		)
	}
	expectedRanges := []SegmentRange{
		SegmentRange{16, 64, 0},
		SegmentRange{80, 128, 48},
	}
	if !slices.Equal(expectedRanges, fragment.Ranges) {
		t.Fatalf(
			"fragment has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment.Ranges),
			digestSegmentRanges(expectedRanges),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_TwoSegmentsSplitBecauseGapMax(t *testing.T) {
	ms1 := makeSeq(0, 16*2)     // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2)  // <- gap
	ms3 := makeSeq(32*2, 16*2)  // <- speech
	ms4 := makeSeq(48*2, 16*2)  // <- gap
	ms5 := makeSeq(64*2, 16*2)  // <- gap, will be cut
	ms6 := makeSeq(80*2, 16*2)  // <- gap
	ms7 := makeSeq(96*2, 16*2)  // <- speech
	ms8 := makeSeq(112*2, 16*2) // <- gap
	ms9 := makeSeq(128*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8, ms9)
	fragments, err := SegmentAudioByTimestampRanges(
		audio,
		[]speech.Segment{
			speech.Segment{
				SpeechStartAt: 0.002,
				SpeechEndAt:   0.003,
			},
			speech.Segment{
				SpeechStartAt: 0.006,
				SpeechEndAt:   0.007,
			},
		},
		1, // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
		3, // gap max in ms
		120_000,
	)
	if err != nil {
		t.Fatal("error in SegmentAudioByTimestampRanges")
	}
	if len(fragments) != 2 {
		t.Fatalf("%d fragments instead of 2", len(fragments))
	}

	fragment1 := fragments[0]
	expectedContent1 := slices.Concat(ms2, ms3, ms4)
	if !slices.Equal(expectedContent1, fragment1.Content) {
		t.Fatalf(
			"fragment 1 has wrong content: %s instead of %s",
			digestBytes(fragment1.Content),
			digestBytes(audio),
		)
	}
	expectedRanges1 := []SegmentRange{
		SegmentRange{16, 64, 0},
	}
	if !slices.Equal(expectedRanges1, fragment1.Ranges) {
		t.Fatalf(
			"fragment 1 has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment1.Ranges),
			digestSegmentRanges(expectedRanges1),
		)
	}

	fragment2 := fragments[1]
	expectedContent2 := slices.Concat(ms6, ms7, ms8)
	if !slices.Equal(expectedContent2, fragment2.Content) {
		t.Fatalf(
			"fragment 2 has wrong content: %s instead of %s",
			digestBytes(fragment2.Content),
			digestBytes(audio),
		)
	}
	expectedRanges2 := []SegmentRange{
		SegmentRange{80, 128, 0},
	}
	if !slices.Equal(expectedRanges2, fragment2.Ranges) {
		t.Fatalf(
			"fragment 2 has wrong ranges: %s instead of %s",
			digestSegmentRanges(fragment2.Ranges),
			digestSegmentRanges(expectedRanges2),
		)
	}
}

func Test_SegmentAudioByTimestampRanges_TwoSegmentsSplitBecauseFragmentMax(t *testing.T) {
	ms1 := makeSeq(0, 16*2)     // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2)  // <- gap
	ms3 := makeSeq(32*2, 16*2)  // <- speech
	ms4 := makeSeq(48*2, 16*2)  // <- gap
	ms5 := makeSeq(64*2, 16*2)  // <- gap, will be cut
	ms6 := makeSeq(80*2, 16*2)  // <- gap
	ms7 := makeSeq(96*2, 16*2)  // <- speech
	ms8 := makeSeq(112*2, 16*2) // <- gap
	ms9 := makeSeq(128*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8, ms9)

	fragmentMaxes := [5]int{1, 2, 3, 4, 5}
	fragmentCounts := [5]int{2, 2, 2, 1, 1}

	for varIndex := range 5 {
		fragments, err := SegmentAudioByTimestampRanges(
			audio,
			[]speech.Segment{
				speech.Segment{
					SpeechStartAt: 0.002,
					SpeechEndAt:   0.003,
				},
				speech.Segment{
					SpeechStartAt: 0.006,
					SpeechEndAt:   0.007,
				},
			},
			1,                       // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
			2000,                    // gap max in ms
			fragmentMaxes[varIndex], // fragment max in ms
		)
		if err != nil {
			t.Fatal("error in SegmentAudioByTimestampRanges")
		}
		if len(fragments) != fragmentCounts[varIndex] {
			t.Fatalf(
				"%d fragments instead of %d for variant %d",
				len(fragments),
				fragmentCounts[varIndex],
				varIndex,
			)
		}
	}
}

func Test_SegmentAudioByTimestampRanges_AudioLength(t *testing.T) {
	ms1 := makeSeq(0, 16*2)     // <- gap, will be cut
	ms2 := makeSeq(16*2, 16*2)  // <- gap
	ms3 := makeSeq(32*2, 16*2)  // <- speech
	ms4 := makeSeq(48*2, 16*2)  // <- gap
	ms5 := makeSeq(64*2, 16*2)  // <- gap, will be cut
	ms6 := makeSeq(80*2, 16*2)  // <- gap
	ms7 := makeSeq(96*2, 16*2)  // <- speech
	ms8 := makeSeq(112*2, 16*2) // <- gap
	ms9 := makeSeq(128*2, 16*2) // <- gap, will be cut

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8, ms9)

	for audioLength := range 32 {
		fragments, err := SegmentAudioByTimestampRanges(
			audio[:audioLength],
			[]speech.Segment{
				speech.Segment{
					SpeechStartAt: 0.000,
					SpeechEndAt:   0.003,
				},
			},
			1,       // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
			2000,    // gap max in ms
			120_000, // fragment max in ms
		)
		if audioLength%2 != 0 {
			if !errors.Is(err, ErrAudioNotMultipleOf16Bits) {
				t.Fatalf("ErrAudioNotMultipleOf16Bits must occur for odd len %d: %v", audioLength, err)
			}
			continue
		}
		if audioLength < 2 {
			if !errors.Is(err, ErrSegmentStartTimestampOutOfRange) {
				t.Fatalf("ErrSegmentStartTimestampOutOfRange must occur for short len %d: %v", audioLength, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("error in SegmentAudioByTimestampRanges: %v", err)
		}
		expected := 0
		if audioLength > 0 {
			expected = 1
		}
		if expected != len(fragments) {
			t.Fatalf(
				"%d fragments instead of %d for audio length %d",
				len(fragments),
				expected,
				audioLength,
			)
		}
	}
}

func Test_SegmentAudioByTimestampRanges_ErrEndOutOfRange(t *testing.T) {
	ms1 := makeSeq(0, 16*2)
	ms2 := makeSeq(16*2, 16*2)
	ms3 := makeSeq(32*2, 16*2)
	ms4 := makeSeq(48*2, 16*2)
	ms5 := makeSeq(64*2, 16*2)
	ms6 := makeSeq(80*2, 16*2)
	ms7 := makeSeq(96*2, 16*2)
	ms8 := makeSeq(112*2, 16*2)
	ms9 := makeSeq(128*2, 16*2)

	audio := slices.Concat(ms1, ms2, ms3, ms4, ms5, ms6, ms7, ms8, ms9)

	speechEnds := [3]float64{0.001, 0.002, 0.003}
	expectErrors := [3]bool{true, true, false}

	for i := range 3 {
		_, err := SegmentAudioByTimestampRanges(
			audio,
			[]speech.Segment{
				speech.Segment{
					SpeechStartAt: 0.002,
					SpeechEndAt:   speechEnds[i],
				},
			},
			1,       // padding is 1ms = 16 frames (at 16kHz) = 16*2 bytes
			2000,    // gap max in ms
			120_000, // fragment max in ms
		)
		if expectErrors[i] {
			if !errors.Is(err, ErrSegmentEndTimestampOutOfRange) {
				t.Fatalf("ErrSegmentEndTimestampOutOfRange must occur: %v", err)
			}
		} else {
			if err != nil {
				t.Fatalf("error occurred: %v", err)
			}
		}
	}
}
