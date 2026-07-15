package subtitles

import "strings"

type InputWord struct {
	Word    string
	StartMs int
	EndMs   int
	Speaker uint32
}

type OutputWord struct {
	Word    string `json:"word"`
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
}

type Subtitle struct {
	Words   []OutputWord `json:"words"`
	StartMs int          `json:"start_ms"`
	EndMs   int          `json:"end_ms"`
}

func (s *Subtitle) JoinText() string {
	words := make([]string, 0, len(s.Words))
	for i := range s.Words {
		words = append(words, s.Words[i].Word)
	}
	return strings.Join(words, " ")
}

func joinWords(words []InputWord) Subtitle {
	n := len(words)
	if n == 0 {
		return Subtitle{}
	}
	outputWords := make([]OutputWord, 0, n)
	for i := range words {
		outputWords = append(outputWords, OutputWord{
			Word:    words[i].Word,
			StartMs: words[i].StartMs,
			EndMs:   words[i].EndMs,
		})
	}
	return Subtitle{
		Words:   outputWords,
		StartMs: words[0].StartMs,
		EndMs:   words[len(words)-1].EndMs,
	}
}

/*
 * Group input words into subtitles.
 * A group is split if any pause between its words exceeds `maxPauseMs`
 * or the word count gets over `maxWords`.
 */
func Group(words []InputWord, maxPauseMs int, maxWords int) []Subtitle {
	result := make([]Subtitle, 0)
	if len(words) == 0 {
		return result
	}
	start := 0
	end := start + 1

	for end < len(words) {
		prev := end - 1

		if words[prev].EndMs+maxPauseMs < words[end].StartMs || words[prev].Speaker != words[end].Speaker || end-start >= maxWords {
			result = append(result, joinWords(words[start:end]))
			start = end
			end = start + 1
			continue
		}

		end += 1
	}

	result = append(result, joinWords(words[start:]))

	return result
}
