package llm

import (
	"encoding/json"
	"fmt"

	dbgen "github.com/mxwell/qarau/db/gen"
)

type WordBreakdown struct {
	Word            string `json:"word"`
	PartOfSpeech    string `json:"pos"`
	Base            string `json:"base"`
	BaseTranslation string `json:"base_translation"`
	WordTranslation string `json:"word_translation"`
	Comment         string `json:"comment"`
}

type SentBreakdown struct {
	Seq          int32           `json:"seq"`
	StartMs      int32           `json:"start_ms"`
	EndMs        int32           `json:"end_ms"`
	Text         string          `json:"text"`
	Translations []string        `json:"translations"`
	Words        []WordBreakdown `json:"words"`
}

type BreakdownContent struct {
	Seq          int32
	Translations []string
	Words        []WordBreakdown
}

type PlainTranslations struct {
	Variants []string `json:"variants"`
}

type PlainBreakdown struct {
	Words []WordBreakdown `json:"words"`
}

func ToBreakdownContent(rows []dbgen.GetSentenceBreakdownsRow) ([]BreakdownContent, error) {
	result := make([]BreakdownContent, 0)
	for _, row := range rows {
		var plainTranslations PlainTranslations
		if err := json.Unmarshal(row.Translations, &plainTranslations); err != nil {
			return nil, fmt.Errorf("translations jsonb parse fail at sent seq %d: %w", row.SentenceSeq, err)
		}
		var plainBreakdown PlainBreakdown
		if err := json.Unmarshal(row.Breakdown, &plainBreakdown); err != nil {
			return nil, fmt.Errorf("breakdown jsonb parse fail at sent seq %d: %w", row.SentenceSeq, err)
		}
		result = append(result, BreakdownContent{
			Seq:          row.SentenceSeq,
			Translations: plainTranslations.Variants,
			Words:        plainBreakdown.Words,
		})
	}
	return result, nil
}

func FromWordGenericV1(words []WordGenericV1) []WordBreakdown {
	result := make([]WordBreakdown, 0, len(words))
	for _, word := range words {
		result = append(result, WordBreakdown{
			Word:            word.Word,
			PartOfSpeech:    word.PartOfSpeech,
			Base:            word.Base,
			BaseTranslation: word.BaseTranslation,
			WordTranslation: word.WordTranslation,
			Comment:         word.Comment,
		})
	}
	return result
}
