package eleven

import (
	"log/slog"

	"github.com/mxwell/qarau/internal/vad"
)

// Greedy
func partitionFragments(logger *slog.Logger, fragments []vad.Fragment, targetFrames int) [][]vad.Fragment {
	result := make([][]vad.Fragment, 0)
	n := len(fragments)
	if n == 0 {
		logger.Error("empty input", "func", "partitionFragments")
		return nil
	}
	if targetFrames <= 0 {
		logger.Error("targetFrames is zero", "func", "partitionFragments")
		return nil
	}

	start := -1
	curFrames := 0
	curPartitionOffset := 0
	for i := 0; i < n; i++ {
		if start == -1 {
			start = i
			curFrames = fragments[i].LengthInFrames()
		} else {
			curFrames += fragments[i].LengthInFrames()
		}
		if curFrames >= targetFrames || i+1 == n {
			result = append(result, fragments[start:i+1])
			curPartitionOffset += curFrames
			start = -1
			curFrames = 0
		}
	}
	if start >= 0 {
		panic("start must be -1 outside the loop")
	}
	return result
}
