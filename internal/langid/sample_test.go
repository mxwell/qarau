package langid

import (
	"log/slog"
	"math"
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

func Test_SampleForLangId_TooShort(t *testing.T) {
	frames := (LangIdSampleMinLength - 1)
	fragments := []vad.Fragment{
		{
			Content: make([]byte, frames*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: 0, RealEnd: frames, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
	}

	samples, err := SampleForLangID(getLog(), fragments)
	if err == nil {
		t.Fatalf("too short audio must lead to an error")
	} else if samples != nil {
		t.Fatalf("samples must be nil when error")
	}
}

func Test_SampleForLangId_GoodLengthForOne(t *testing.T) {
	lengths := []int{LangIdSampleMinLength, LangIdSampleMinLength + 1, LangIdSampleMaxLength - 1, LangIdSampleMaxLength}

	for _, length := range lengths {
		fragments := []vad.Fragment{
			{
				Content: make([]byte, length*audio.BytesPer16bitFrame),
				Ranges: []vad.SegmentRange{
					{RealStart: 0, RealEnd: length, CopyStart: 0},
				},
				RealSpeechEnd: -1,
			},
		}

		samples, err := SampleForLangID(getLog(), fragments)
		if err != nil {
			t.Fatalf("sampling failed for sufficiently long input: %v", err)
		}
		if len(samples) != 1 {
			t.Fatalf("%d samples instead of 1", len(samples))
		}
		if len(samples[0]) != length {
			t.Fatalf("%d frames in the first sample instead of %d", len(samples[0]), length)
		}
	}
}

func Test_SampleForLangId_GoodLengthForTwo(t *testing.T) {
	fragments := []vad.Fragment{
		{
			Content: make([]byte, LangIdSampleMaxLength*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: 0, RealEnd: LangIdSampleMaxLength, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
		{
			Content: make([]byte, LangIdSampleMaxLength*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: LangIdSampleMaxLength, RealEnd: 2 * LangIdSampleMaxLength, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
	}

	samples, err := SampleForLangID(getLog(), fragments)
	if err != nil {
		t.Fatalf("sampling failed for sufficiently long input: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("%d samples instead of 2", len(samples))
	}
	for i := range samples {
		if len(samples[i]) != LangIdSampleMaxLength {
			t.Fatalf("%d frames in the sample #%d instead of %d", len(samples[i]), i, LangIdSampleMaxLength)
		}
	}
}

func Test_SampleForLangId_GoodLengthForThree(t *testing.T) {
	fragments := []vad.Fragment{
		{
			Content: make([]byte, LangIdSampleMaxLength*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: 0, RealEnd: LangIdSampleMaxLength, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
		{
			Content: make([]byte, LangIdSampleMaxLength*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: LangIdSampleMaxLength, RealEnd: 2 * LangIdSampleMaxLength, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
		{
			Content: make([]byte, LangIdSampleMaxLength*audio.BytesPer16bitFrame),
			Ranges: []vad.SegmentRange{
				{RealStart: 2 * LangIdSampleMaxLength, RealEnd: 3 * LangIdSampleMaxLength, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		},
	}

	samples, err := SampleForLangID(getLog(), fragments)
	if err != nil {
		t.Fatalf("sampling failed for sufficiently long input: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("%d samples instead of 3", len(samples))
	}
	for i := range samples {
		if len(samples[i]) != LangIdSampleMaxLength {
			t.Fatalf("%d frames in the sample #%d instead of %d", len(samples[i]), i, LangIdSampleMaxLength)
		}
	}
}

func Test_SampleForLangId_LongInput(t *testing.T) {
	fragments := make([]vad.Fragment, 0)

	lengths := []int{1, 2, 3, 5, 4, 2, 5}

	realOffset := 0
	for i, lengthMult := range lengths {
		length := lengthMult * LangIdSampleMaxLength
		content := make([]byte, length*audio.BytesPer16bitFrame)
		for j := range content {
			content[j] = byte(i + 34)
		}
		fragments = append(fragments, vad.Fragment{
			Content: content,
			Ranges: []vad.SegmentRange{
				{RealStart: realOffset, RealEnd: realOffset + length, CopyStart: 0},
			},
			RealSpeechEnd: -1,
		})
		realOffset += length
	}

	samples, err := SampleForLangID(getLog(), fragments)
	if err != nil {
		t.Fatalf("sampling failed for long input: %v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("%d samples instead of 3", len(samples))
	}
	expectedLengths := []int{1, 1, 1}
	expectedValues := []float32{
		audio.MakeF32OfBytes(34, 34),
		audio.MakeF32OfBytes(40, 40),
		audio.MakeF32OfBytes(37, 37),
	}
	for i := range 3 {
		expectedLength := LangIdSampleMaxLength * expectedLengths[i]
		if len(samples[i]) != expectedLength {
			t.Fatalf("%d frames in the sample #%d instead of %d", len(samples[i]), i, expectedLength)
		}
		for j := range samples[i] {
			if math.Abs(float64(samples[i][j]-expectedValues[i])) > 1e-6 {
				t.Fatalf("expected float32 %f instead of %f in samples[%d][%d]", expectedValues[i], samples[i][j], i, j)
			}
		}
	}
}
