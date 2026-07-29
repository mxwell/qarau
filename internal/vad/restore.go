package vad

import (
	"log/slog"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

func RestoreWordTimestamps(
	log *slog.Logger,
	ranges []SegmentRange,
	words []qarauv1.Word,
) []qarauv1.Word {
	result := make([]qarauv1.Word, 0, len(words))

	pos := 0
	for i := range words {
		word := &words[i]
		wordStart := int(word.StartMs * SampleRateKhz)
		wordEnd := int(word.EndMs * SampleRateKhz)

		for pos+1 < len(ranges) && ranges[pos+1].CopyStart <= wordStart {
			pos += 1
		}

		if pos >= len(ranges) {
			log.Warn(
				"word start is out of range",
				"wordStart", wordStart,
				"i", i,
				"word", word.Word,
				"ranges", len(ranges),
			)
			break
		}

		r := ranges[pos]
		span := r.GetDuration()
		copyStart := r.CopyStart
		copyEnd := copyStart + span

		if copyStart <= wordStart && wordEnd <= copyEnd {
			delta := r.RealStart - copyStart
			if delta < 0 {
				log.Warn(
					"negative delta in RestoreWordTimestamps",
					"i", i,
					"delta", delta,
					"word", word.Word,
				)
				continue
			}
			result = append(result, qarauv1.Word{
				Word:       word.Word,
				StartMs:    uint32(wordStart+delta) / SampleRateKhz,
				EndMs:      uint32(wordEnd+delta) / SampleRateKhz,
				Confidence: word.Confidence,
			})
		} else {
			log.Warn(
				"word timestamps don't fit the range",
				"i", i,
				"word", word.Word,
				"copyStart", copyStart,
				"wordStart", wordStart,
				"copyEnd", copyEnd,
				"wordEnd", wordEnd,
			)
		}
	}

	return result
}
