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

func (l lang) MarshalText() ([]byte, error) {
	switch l {
	case langKz:
		return []byte("kz"), nil
	case langRu:
		return []byte("ru"), nil
	default:
		return []byte("other"), nil
	}
}

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

// Marks elements of a and b that belong to one longest common subsequence,
// returns the marks and the subsequence length.
func lcsAlign(a, b []string) ([]bool, []bool, int) {
	// dp[i][j] = LCS length of a[i:] and b[j:]
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	inA := make([]bool, len(a))
	inB := make([]bool, len(b))
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			inA[i], inB[j] = true, true
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return inA, inB, dp[0][0]
}
