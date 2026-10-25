package main

import (
	"math"
	"slices"
	"testing"

	"github.com/mxwell/qarau/internal/pricing"
)

func Test_tokenize(t *testing.T) {
	got := tokenize("Е, Мәлен сенің ұятың жоқ еді ғой — айтпақшы.")
	want := []string{"е", "мәлен", "сенің", "ұятың", "жоқ", "еді", "ғой", "айтпақшы"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = tokenize("оқу-уқыбастау, «меңгеру»")
	want = []string{"оқу-уқыбастау", "меңгеру"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func Test_classifyLang(t *testing.T) {
	cases := map[string]lang{
		"Мен айтып жатырмын.":                         langKz,
		"Не di di дейді.":                             langKz,
		"Қызғаныш деген зависть, иә.":                 langKz,
		"Он бірінші сыныптар өтіп бара жатқан кезде.": langKz,
		"Для вас это все бесплатно.":                  langRu,
		"Если я здесь родился, как ее не будет?":      langRu,
		"Начальник, обижаешь.":                        langRu,
		"Ты в погонах не разбираешься что ли?":        langRu,
		"Нет, сестричка.":                             langRu,
		"OK":                                          langOther,
	}
	for s, want := range cases {
		if got := classifyLang(tokenize(s)); got != want {
			t.Errorf("%q: got %d, want %d", s, got, want)
		}
	}
}

func Test_lcsAlign(t *testing.T) {
	a := []string{"жаңа", "оқулықты", "оқып", "отырмын"}
	if _, _, n := lcsAlign(a, a); n != 4 {
		t.Fatalf("identical: got %d", n)
	}
	// skipped word and a made-up one
	b := []string{"жаңа", "кітапты", "оқып", "отырмын"}
	inA, inB, n := lcsAlign(a, b)
	if n != 3 {
		t.Fatalf("got %d, want 3", n)
	}
	if !slices.Equal(inA, []bool{true, false, true, true}) || !slices.Equal(inB, []bool{true, false, true, true}) {
		t.Fatalf("wrong marks: %v %v", inA, inB)
	}
	if _, _, n := lcsAlign(a, nil); n != 0 {
		t.Fatalf("empty: got %d", n)
	}
}

func Test_checkSentence(t *testing.T) {
	c := checkSentence("Жаңа оқулықты оқып отырмын.", SentenceOutput{
		Sentence:     "Жаңа оқулықты оқып отырмын.",
		Translations: []string{"a"},
		Breakdown: []WordOutput{
			{Word: "Жаңа оқулықты", Base: "жаңа оқулық", Comment: "аффикс -ты"},
			{Word: "оқып", Base: "оқу"},
			{Word: "жатыр", Base: "жату"},
		},
	})
	if c.Missing || !c.EchoOK || c.Lang != langKz || c.Words != 4 || c.Items != 3 {
		t.Fatalf("unexpected basics: %+v", c)
	}
	if !slices.Equal(c.SkippedWords, []string{"отырмын"}) || !slices.Equal(c.ExtraWords, []string{"жатыр"}) {
		t.Fatalf("skipped %q, extra %q", c.SkippedWords, c.ExtraWords)
	}
	if c.LowGran || !c.FewTranslations {
		t.Fatalf("low_gran %v, few_translations %v", c.LowGran, c.FewTranslations)
	}
	want := []itemCheck{
		{Affixed: true, HasComment: true, NamesAffix: true},
		{Affixed: true},
		{Affixed: true},
	}
	if !slices.Equal(c.ItemChecks, want) {
		t.Fatalf("item checks: %+v", c.ItemChecks)
	}

	ru := checkSentence("Для вас это все бесплатно.", SentenceOutput{Sentence: "Для вас это все бесплатно."})
	if ru.Lang != langRu || ru.ItemChecks != nil || ru.LowGran {
		t.Fatalf("ru sentence must skip word-level checks: %+v", ru)
	}
}

func Test_affixRe(t *testing.T) {
	for _, s := range []string{"аффикс -ты", "«-ға» барыс септік", "-мын"} {
		if !affixRe.MatchString(s) {
			t.Errorf("must match: %q", s)
		}
	}
	for _, s := range []string{"", "оқу-уқыбастау сөзі", "Винительный падеж"} {
		if affixRe.MatchString(s) {
			t.Errorf("must not match: %q", s)
		}
	}
}

func Test_scoreRecord(t *testing.T) {
	rec := RunRecord{
		Model:     "m",
		Sentences: []string{"Жаңа оқулықты оқып отырмын.", "Иә?", "Для вас это все бесплатно."},
		Output: []SentenceOutput{
			{
				Sentence:     "Жаңа оқулықты оқып отырмын.",
				Translations: []string{"a", "b"},
				Breakdown: []WordOutput{
					{Word: "Жаңа", Base: "жаңа"},
					{Word: "оқулықты", Base: "оқулық", Comment: "аффикс -ты"},
					{Word: "оқып отырмын", Base: "оқу", Comment: ""},
				},
			},
			{},
			{Sentence: "Для вас это всё бесплатно."},
		},
		InputTokens:  3000,
		OutputTokens: 600,
	}
	s := scoreRecord(rec, map[string]pricing.Price{"m": {Input: 1, Output: 10}})

	check := func(key string, want float64) {
		t.Helper()
		got, ok := s[key].value()
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: got %v (ok=%v), want %v", key, got, ok, want)
		}
	}
	check("calls_ok", 1)
	check("missing", 1.0/3)
	check("echo_ok", 0.5) // ё vs е counts as a mismatch
	check("coverage", 1)
	check("granularity", 0.75)
	check("low_gran", 0)
	check("comment_present", 0.5)
	check("comment_affix", 0.5)
	check("two_tr", 1)
	check("out_tok", 200)
	check("cost", 100*(3000*1+600*10)/1e6/3)
}
