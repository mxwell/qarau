package sentences

import (
	"regexp"
	"strings"
	"unicode"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

type Sentence struct {
	StartWordSeq int
	EndWordSeq   int // inclusive
	StartMs      uint32
	EndMs        uint32
	Text         string
}

const (
	maxWordPerSentence = 40
)

var (
	terminalWordPattern = regexp.MustCompile(`[.!?…]+$`)
)

func isTrailingNoise(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(`»"”'’)]`, r)
}

func joinWords(words []*qarauv1.Word) string {
	parts := make([]string, len(words))
	for i := range words {
		parts[i] = words[i].Word
	}
	return strings.Join(parts, " ")
}

func SegmentWordStream(words []*qarauv1.Word) []Sentence {
	result := make([]Sentence, 0)

	n := len(words)
	for i := 0; i < n; {
		j := i
		for j-i+1 < maxWordPerSentence && j+1 < n {
			next := j + 1
			if words[i].Speaker != words[next].Speaker {
				break
			}
			word := words[j].Word
			trimmed := strings.TrimRightFunc(word, isTrailingNoise)
			if terminalWordPattern.MatchString(trimmed) {
				break
			}
			j = next
		}
		result = append(result, Sentence{
			StartWordSeq: i,
			EndWordSeq:   j,
			StartMs:      words[i].StartMs,
			EndMs:        words[j].EndMs,
			Text:         joinWords(words[i : j+1]),
		})
		i = j + 1
	}

	return result
}
