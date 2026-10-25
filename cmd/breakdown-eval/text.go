package main

import (
	"regexp"
	"strings"
	"unicode"
)

type lang int

const (
	langKz lang = iota
	langRu
	// no Cyrillic at all
	langOther
)

const (
	kazakhLetters = "әғқңөұүһі"
	// Kazakh native words don't use these
	russianLetters = "ёщъьэ"
)

// Frequent Russian words that are not Kazakh words.
// Deliberately excludes "не", "он", "да": they are common in Kazakh too.
var russianWords = map[string]bool{
	"я": true, "ты": true, "она": true, "мы": true, "вы": true, "они": true,
	"и": true, "в": true, "на": true, "с": true, "у": true, "по": true, "для": true,
	"что": true, "это": true, "все": true, "как": true, "так": true, "же": true,
	"бы": true, "ли": true, "нет": true, "если": true, "здесь": true, "просто": true,
	"конечно": true, "меня": true, "тебя": true, "вас": true, "его": true, "ее": true,
	"был": true, "было": true, "была": true, "или": true, "но": true, "а": true,
	"пожалуйста": true, "очень": true, "уже": true, "еще": true, "только": true,
}

// "-ты", "«-ға»", but not a hyphen inside a compound word
var affixRe = regexp.MustCompile(`(?:^|[^\p{L}])-\p{L}+`)

func trimToken(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}

// Lowercased words without surrounding punctuation; hyphenated words stay whole.
func tokenize(s string) []string {
	fields := strings.Fields(strings.ToLower(s))
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := trimToken(f); t != "" {
			result = append(result, t)
		}
	}
	return result
}

func normalizeText(s string) string {
	return strings.Join(tokenize(s), " ")
}

func isCyrillic(token string) bool {
	for _, r := range token {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// Rough classification of code-switched subtitles: Russian wins only
// with more Russian markers than Kazakh ones.
func classifyLang(tokens []string) lang {
	kz, ru, cyr := 0, 0, 0
	for _, t := range tokens {
		if !isCyrillic(t) {
			continue
		}
		cyr++
		if strings.ContainsAny(t, kazakhLetters) {
			kz++
		} else if russianWords[t] || strings.ContainsAny(t, russianLetters) {
			ru++
		}
	}
	if cyr == 0 {
		return langOther
	}
	if ru > kz {
		return langRu
	}
	return langKz
}

// length of the longest common subsequence
func lcs(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(prev[j+1], cur[j])
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
