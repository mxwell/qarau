package langid

import (
	"fmt"
	"log/slog"

	"github.com/mxwell/qarau/internal/vad"
)

// Collect 1-3 samples: sample is a contiguous range of fragments converted to float32 frames
// One approach is to take 1st sample from the beginning, 2nd sample at the end, 3rd sample from the middle
func SampleForLangID(logger *slog.Logger, fragments []vad.Fragment) ([][]float32, error) {
	result := make([][]float32, 0, 3)

	n := len(fragments)

	prefix := 0
	prefixLength := 0 // in frames

	for prefix < n && prefixLength < LangIdSampleMaxLength {
		prefixLength += fragments[prefix].LengthInFrames()
		prefix++
	}

	if prefixLength < LangIdSampleMinLength {
		return nil, fmt.Errorf("failed to sample for langid: prefix length %d frames", prefixLength)
	}

	first, err := vad.StitchAsFloat32(fragments[:prefix], LangIdSampleMaxLength)
	if err != nil {
		return nil, fmt.Errorf("stitchAsFloat32 fail: %w", err)
	}
	logger.Info("first sample for langid", "prefix", prefix, "frames", len(first))
	result = append(result, first)

	suffix := n
	suffixLength := 0 // in frames

	for suffix > prefix && suffixLength < LangIdSampleMaxLength {
		suffixLength += fragments[suffix-1].LengthInFrames()
		suffix--
	}

	if suffixLength < LangIdSampleMinLength {
		return result, nil
	}

	second, err := vad.StitchAsFloat32(fragments[suffix:], LangIdSampleMaxLength)
	if err != nil {
		return nil, fmt.Errorf("stitchAsFloat32 fail: %w", err)
	}
	logger.Info("second sample for langid", "suffix", suffix, "frames", len(second))
	result = append(result, second)

	lf := (prefix + suffix) / 2
	rg := lf + 1
	midLength := 0 // in frames
	if lf >= prefix && lf < suffix {
		midLength += fragments[lf].LengthInFrames()
	} else {
		return result, nil
	}

	for lf >= prefix && rg <= suffix && midLength < LangIdSampleMaxLength {
		extended := false
		if lf-1 >= prefix {
			midLength += fragments[lf-1].LengthInFrames()
			lf--
			extended = true
		}
		if rg+1 <= suffix && midLength < LangIdSampleMaxLength {
			midLength += fragments[rg].LengthInFrames()
			rg++
			extended = true
		}
		if !extended {
			break // stop if no movement
		}
	}

	if midLength < LangIdSampleMinLength {
		return result, nil
	}

	third, err := vad.StitchAsFloat32(fragments[lf:rg], LangIdSampleMaxLength)
	if err != nil {
		return nil, fmt.Errorf("stitchAsFloat32 fail: %w", err)
	}
	logger.Info("third sample for langid", "lf", lf, "rg", rg, "frames", len(third))
	result = append(result, third)
	return result, nil
}
