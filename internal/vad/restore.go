package vad

import (
	"log/slog"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

const (
	WordResultGood = iota
	WordResultFixedStart
	WordResultFixedEnd
)

func RestoreWordTimestamps(
	log *slog.Logger,
	ranges []SegmentRange,
	words []*qarauv1.Word,
) []*qarauv1.Word {
	result := make([]*qarauv1.Word, 0, len(words))
	wordResults := make([]int, 0, len(words))

	if len(ranges) == 0 {
		return result
	}

	pos := 0
	for i := range words {
		word := words[i]
		wordStart := int(word.StartMs * SampleRateKhz)
		wordEnd := int(word.EndMs * SampleRateKhz)
		if wordEnd <= wordStart {
			log.Warn(
				"fixing wordEnd",
				"func", "RestoreWordTimestamps",
				"old", wordEnd,
				"new", wordStart+1,
				"wordStart", wordStart,
			)
			wordEnd = wordStart + 1
		}

		for pos+1 < len(ranges) && ranges[pos+1].CopyStart <= wordStart {
			pos++
		}

		r := ranges[pos]
		span := r.Duration()
		copyStart := r.CopyStart
		copyEnd := copyStart + span

		delta := r.RealStart - copyStart
		if delta < 0 {
			log.Warn(
				"negative delta",
				"func", "RestoreWordTimestamps",
				"i", i,
				"delta", delta,
				"word", word.Word,
			)
			continue
		}

		if copyStart <= wordStart && wordEnd <= copyEnd {
			result = append(result, &qarauv1.Word{
				Word:       word.Word,
				StartMs:    uint32(wordStart+delta) / SampleRateKhz,
				EndMs:      uint32(wordEnd+delta) / SampleRateKhz,
				Confidence: word.Confidence,
			})
			wordResults = append(wordResults, WordResultGood)
		} else if copyStart <= wordStart && wordStart < copyEnd {
			log.Warn(
				"only wordStart fits into range",
				"func", "RestoreWordTimestamps",
				"i", i,
				"word", word.Word,
				"copyStart", copyStart,
				"wordStart", wordStart,
				"copyEnd", copyEnd,
				"wordEnd", wordEnd,
			)
			result = append(result, &qarauv1.Word{
				Word:       word.Word,
				StartMs:    uint32(wordStart+delta) / SampleRateKhz,
				EndMs:      uint32(copyEnd+delta) / SampleRateKhz,
				Confidence: word.Confidence,
			})
			wordResults = append(wordResults, WordResultFixedEnd)
		} else if copyStart < wordEnd && wordEnd <= copyEnd {
			log.Warn(
				"only wordEnd fits into range",
				"func", "RestoreWordTimestamps",
				"i", i,
				"word", word.Word,
				"copyStart", copyStart,
				"wordStart", wordStart,
				"copyEnd", copyEnd,
				"wordEnd", wordEnd,
			)
			result = append(result, &qarauv1.Word{
				Word:       word.Word,
				StartMs:    uint32(copyStart+delta) / SampleRateKhz,
				EndMs:      uint32(wordEnd+delta) / SampleRateKhz,
				Confidence: word.Confidence,
			})
			wordResults = append(wordResults, WordResultFixedStart)
		} else {
			log.Warn(
				"word timestamps don't fit the range",
				"func", "RestoreWordTimestamps",
				"i", i,
				"word", word.Word,
				"copyStart", copyStart,
				"wordStart", wordStart,
				"copyEnd", copyEnd,
				"wordEnd", wordEnd,
			)
		}
	}

	for i := range result {
		if wordResults[i] == WordResultFixedStart {
			if i > 0 && result[i-1].EndMs > result[i].StartMs {
				log.Warn("fixing StartMs guess",
					"func", "RestoreWordTimestamps",
					"i", i,
					"word", result[i].Word,
					"old StartMs", result[i].StartMs,
					"prev EndMs", result[i-1].EndMs,
				)
				result[i].StartMs = result[i-1].EndMs
			}
		} else if wordResults[i] == WordResultFixedEnd {
			if i+1 < len(result) && result[i+1].StartMs < result[i].EndMs {
				log.Warn("fixing EndMs guess",
					"func", "RestoreWordTimestamps",
					"i", i,
					"word", result[i].Word,
					"old EndMs", result[i].EndMs,
					"next StartMs", result[i+1].StartMs,
				)
				result[i].EndMs = result[i+1].StartMs
			}
		}
	}

	return result
}
