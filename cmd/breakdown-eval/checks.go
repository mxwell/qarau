package main

import "strings"

type itemCheck struct {
	// word differs from its base form, so it should carry an affix comment
	Affixed    bool `json:"affixed"`
	HasComment bool `json:"has_comment"`
	NamesAffix bool `json:"names_affix"`
}

// Per-sentence checks shared by score and view. Word-level fields are filled
// only for kz sentences; granularity and translation checks only for multi-word ones.
type sentenceCheck struct {
	Missing         bool        `json:"missing"`
	EchoOK          bool        `json:"echo_ok"`
	Lang            lang        `json:"lang"`
	Words           int         `json:"words"`
	Items           int         `json:"items"`
	Matched         int         `json:"matched"`
	OutWords        int         `json:"out_words"`
	LowGran         bool        `json:"low_gran"`
	FewTranslations bool        `json:"few_translations"`
	SkippedWords    []string    `json:"skipped_words,omitempty"`
	ExtraWords      []string    `json:"extra_words,omitempty"`
	ItemChecks      []itemCheck `json:"item_checks,omitempty"`
}

func (c sentenceCheck) multiWordKz() bool {
	return c.Lang == langKz && c.Words >= 2
}

func checkSentence(input string, out SentenceOutput) sentenceCheck {
	inTokens := tokenize(input)
	c := sentenceCheck{
		Missing: out.Sentence == "",
		Lang:    classifyLang(inTokens),
		Words:   len(inTokens),
		Items:   len(out.Breakdown),
	}
	if c.Missing {
		return c
	}
	c.EchoOK = normalizeText(out.Sentence) == normalizeText(input)
	if c.Lang != langKz {
		return c
	}

	var outTokens []string
	for _, item := range out.Breakdown {
		outTokens = append(outTokens, tokenize(item.Word)...)
	}
	inMatched, outMatched, matched := lcsAlign(inTokens, outTokens)
	c.Matched = matched
	c.OutWords = len(outTokens)
	for i, ok := range inMatched {
		if !ok {
			c.SkippedWords = append(c.SkippedWords, inTokens[i])
		}
	}
	for i, ok := range outMatched {
		if !ok {
			c.ExtraWords = append(c.ExtraWords, outTokens[i])
		}
	}

	if c.multiWordKz() {
		c.LowGran = 3*c.Items < 2*c.Words
		c.FewTranslations = len(out.Translations) < 2
	}

	c.ItemChecks = make([]itemCheck, len(out.Breakdown))
	for i, item := range out.Breakdown {
		base := normalizeText(item.Base)
		if base == "" || base == normalizeText(item.Word) {
			continue
		}
		c.ItemChecks[i] = itemCheck{
			Affixed:    true,
			HasComment: strings.TrimSpace(item.Comment) != "",
			NamesAffix: affixRe.MatchString(item.Comment),
		}
	}
	return c
}
