package subtitles

import "strings"

type InputWord struct {
	Word    string
	StartMs int
	EndMs   int
}

type Subtitle struct {
	Text    string `json:"text"`
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
}

func joinWords(words []InputWord) Subtitle {
	n := len(words)
	if n == 0 {
		return Subtitle{}
	}
	wordTexts := make([]string, 0, n)
	for i := range words {
		wordTexts = append(wordTexts, words[i].Word)
	}
	return Subtitle{
		Text:    strings.Join(wordTexts, " "),
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

		if words[prev].EndMs+maxPauseMs < words[end].StartMs || end-start >= maxWords {
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
