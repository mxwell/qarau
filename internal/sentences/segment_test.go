package sentences

import (
	"fmt"
	"strconv"
	"testing"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

func checkSentence(actual, expected Sentence, t *testing.T) {
	t.Helper()
	if actual.Text != expected.Text {
		t.Fatalf("sent text %s intead of %s", actual.Text, expected.Text)
	}
	if actual.StartMs != expected.StartMs {
		t.Fatalf("sent start ms %d instead of %d", actual.StartMs, expected.StartMs)
	}
	if actual.EndMs != expected.EndMs {
		t.Fatalf("sent end ms %d instead of %d", actual.EndMs, expected.EndMs)
	}
	if actual.StartWordSeq != expected.StartWordSeq {
		t.Fatalf("sent start word seq %d instead of %d", actual.StartWordSeq, expected.StartWordSeq)
	}
	if actual.EndWordSeq != expected.EndWordSeq {
		t.Fatalf("sent end word seq %d instead of %d", actual.EndWordSeq, expected.EndWordSeq)
	}
}

// checkSplit asserts that `words` is segmented exactly at `bounds`, where bounds
// holds the inclusive end index of every expected sentence. Every field of every
// sentence is derived from `words` and checked, so callers only have to say
// *where* the cuts fall.
func checkSplit(t *testing.T, label string, words []qarauv1.Word, bounds []int) []Sentence {
	t.Helper()

	actual := SegmentWordStream(words)
	if len(actual) != len(bounds) {
		texts := make([]string, len(actual))
		for i := range actual {
			texts[i] = actual[i].Text
		}
		t.Fatalf("%s: got %d sents instead of %d: %q", label, len(actual), len(bounds), texts)
	}

	start := 0
	for i, end := range bounds {
		checkSentence(
			actual[i],
			Sentence{
				StartWordSeq: start,
				EndWordSeq:   end,
				StartMs:      words[start].StartMs,
				EndMs:        words[end].EndMs,
				Text:         joinWords(words[start : end+1]),
			},
			t,
		)
		start = end + 1
	}
	if start != len(words) {
		t.Fatalf("%s: sentences cover %d words out of %d", label, start, len(words))
	}
	return actual
}

func Test_SegmentWordStream(t *testing.T) {
	res0 := SegmentWordStream([]qarauv1.Word{})
	if len(res0) != 0 {
		t.Fatalf("got %d sentences instead of 0 for empty input", len(res0))
	}

	words := []qarauv1.Word{
		qarauv1.Word{
			Word:       "w1",
			StartMs:    500,
			EndMs:      1000,
			Confidence: 100,
			Speaker:    0,
		},
	}
	w1 := &words[0]

	res1 := SegmentWordStream(words)
	if len(res1) != 1 {
		t.Fatalf("got %d sents instead of 1 for 1-word input", len(res1))
	}

	checkSentence(
		res1[0],
		Sentence{
			StartMs:      w1.StartMs,
			EndMs:        w1.EndMs,
			StartWordSeq: 0,
			EndWordSeq:   0,
			Text:         w1.Word,
		},
		t,
	)

	words = append(words, qarauv1.Word{
		Word:       "w2",
		StartMs:    1500,
		EndMs:      2000,
		Confidence: 100,
		Speaker:    0,
	})
	w2 := &words[1]

	res2 := SegmentWordStream(words)
	if len(res2) != 1 {
		t.Fatalf("got %d sents instead of 1 for 2-words input", len(res2))
	}

	checkSentence(
		res2[0],
		Sentence{
			StartMs:      w1.StartMs,
			EndMs:        w2.EndMs,
			StartWordSeq: 0,
			EndWordSeq:   1,
			Text:         w1.Word + " " + w2.Word,
		},
		t,
	)
}

func getNWords(n int) []qarauv1.Word {
	words := make([]qarauv1.Word, 0)
	for i := range n {
		words = append(words, qarauv1.Word{
			Word:    "с" + strconv.Itoa(i),
			StartMs: uint32(i*1000 + 100),
			EndMs:   uint32(i*1000 + 500),
		})
	}
	return words
}

func get5Words() []qarauv1.Word {
	return getNWords(5)
}

func Test_SegmentWordStream_TerminalWords(t *testing.T) {
	words := get5Words()
	res0 := SegmentWordStream(words)
	if len(res0) != 1 {
		t.Fatalf("got %d sents instead of 1 for 5-word input", len(res0))
	}
	expectedText := joinWords(words)
	if res0[0].Text != expectedText {
		t.Fatalf("got text %s instead of %s for 5-word input", res0[0].Text, expectedText)
	}

	terminals := []string{
		".",
		"!",
		"? ",
		"…",
		".»",
		"!)",
		"?' ",
	}

	for _, terminal := range terminals {
		save := words[2].Word

		words[2].Word = save + terminal

		res := SegmentWordStream(words)
		if len(res) != 2 {
			fmt.Println(res[0].Text)
			t.Fatalf("got %d sents instead of 2 for 5-word input with terminal inside", len(res))
		}

		expectedSents := []string{
			joinWords(words[:3]),
			joinWords(words[3:]),
		}
		for i := range 2 {
			if res[i].Text != expectedSents[i] {
				t.Fatalf("got at #%d text %s instead of %s for 5-word input with terminal inside", i, res[i].Text, expectedSents[i])
			}
		}

		words[2].Word = save
	}
}

func Test_SegmentWordStream_SpeakerChange(t *testing.T) {
	words := get5Words()
	for i := range words {
		words[i].Speaker = uint32(i)
	}

	res := SegmentWordStream(words)
	if len(res) != 5 {
		t.Fatalf("got %d sents instead of 5 for input with 5 speakers", len(res))
	}
}

func Test_SegmentWordStream_WordLimit(t *testing.T) {
	w40 := getNWords(40)
	r40 := SegmentWordStream(w40)
	if len(r40) != 1 {
		t.Fatalf("got %d sents instead of 1 for 40-word input", len(r40))
	}
	w41 := getNWords(41)
	r41 := SegmentWordStream(w41)
	if len(r41) != 2 {
		t.Fatalf("got %d sents instead of 2 for 41-word input", len(r41))
	}
}

func Test_SegmentWordStream_NilInput(t *testing.T) {
	res := SegmentWordStream(nil)
	if res == nil {
		t.Fatalf("got nil slice for nil input, want empty non-nil slice")
	}
	if len(res) != 0 {
		t.Fatalf("got %d sentences instead of 0 for nil input", len(res))
	}
}

// The word limit must not only produce the right *number* of sentences: the tail
// sentence has to carry the right seqs and timestamps too, otherwise resolving a
// playback position to a sentence lands on the wrong one.
func Test_SegmentWordStream_WordLimitBoundaries(t *testing.T) {
	checkSplit(t, "39 words", getNWords(39), []int{38})
	checkSplit(t, "40 words", getNWords(40), []int{39})
	checkSplit(t, "41 words", getNWords(41), []int{39, 40})
	checkSplit(t, "80 words", getNWords(80), []int{39, 79})
	checkSplit(t, "81 words", getNWords(81), []int{39, 79, 80})
}

// A terminal early in the stream shifts the whole grid: every later cut is
// relative to the sentence start, not to an absolute multiple of 40.
func Test_SegmentWordStream_WordLimitAfterTerminal(t *testing.T) {
	words := getNWords(60)
	words[5].Word += "."

	checkSplit(t, "60 words, terminal at 5", words, []int{5, 45, 59})
}

func Test_SegmentWordStream_WordLimitAfterSpeakerChange(t *testing.T) {
	words := getNWords(45)
	for i := 42; i < 45; i++ {
		words[i].Speaker = 1
	}

	checkSplit(t, "45 words, speaker change at 42", words, []int{39, 41, 44})
}

func Test_SegmentWordStream_TerminalAtStreamEdges(t *testing.T) {
	first := get5Words()
	first[0].Word += "."
	checkSplit(t, "terminal on first word", first, []int{0, 4})

	// The stream's last word must not spawn a trailing empty sentence.
	last := get5Words()
	last[4].Word += "."
	checkSplit(t, "terminal on last word", last, []int{4})

	single := getNWords(1)
	single[0].Word += "?"
	checkSplit(t, "single terminal word", single, []int{0})
}

func Test_SegmentWordStream_ConsecutiveTerminals(t *testing.T) {
	words := get5Words()
	for i := range words {
		words[i].Word += "."
	}

	checkSplit(t, "every word terminal", words, []int{0, 1, 2, 3, 4})
}

// A punctuation-only token ("." or "…") is still a terminator; a token made up
// only of trailing noise ("»") trims to the empty string and must not split.
func Test_SegmentWordStream_PunctuationOnlyWords(t *testing.T) {
	terminal := get5Words()
	terminal[2].Word = "."
	checkSplit(t, "standalone dot", terminal, []int{2, 4})

	noise := get5Words()
	noise[2].Word = "»"
	checkSplit(t, "standalone closing quote", noise, []int{4})

	empty := get5Words()
	empty[2].Word = ""
	checkSplit(t, "empty word", empty, []int{4})
}

// Punctuation that is not sentence-final — or is sentence-final but not at the
// end of the token — must leave the stream intact.
func Test_SegmentWordStream_NonTerminalPunctuation(t *testing.T) {
	for _, suffix := range []string{
		",",
		";",
		":",
		"-",
		"—",
		")",  // closing bracket with no terminator before it
		"»",  // closing quote with no terminator before it
		"'",  // apostrophe with no terminator before it
		".x", // dot not at the end of the token
		"?x",
	} {
		words := get5Words()
		words[2].Word += suffix

		checkSplit(t, "non-terminal suffix "+strconv.Quote(suffix), words, []int{4})
	}
}

// Extends the terminal table with combinations the plan calls out (`сөз?»`),
// repeated terminators, and every closing character trimmed by isTrailingNoise.
// Unlike Test_SegmentWordStream_TerminalWords this checks seqs and timestamps,
// not just the text.
func Test_SegmentWordStream_TerminalVariants(t *testing.T) {
	for _, terminal := range []string{
		".",
		"!",
		"?",
		"…",
		"...",
		"?!",
		"!!!",
		".»",
		`."`,
		".”",
		".'",
		".’",
		".)",
		".]",
		"…»",
		"?»",
		"? ",
		"?' ",
		"! \t",
		"?»)",
	} {
		words := get5Words()
		words[2].Word += terminal

		checkSplit(t, "terminal "+strconv.Quote(terminal), words, []int{2, 4})
	}
}

func Test_SegmentWordStream_SpeakerChangeBoundaries(t *testing.T) {
	words := get5Words()
	speakers := []uint32{0, 0, 1, 1, 0}
	for i := range words {
		words[i].Speaker = speakers[i]
	}

	checkSplit(t, "speakers 0,0,1,1,0", words, []int{1, 3, 4})
}

// A speaker change and a terminator falling on the same boundary must produce
// one cut, not two (which would leave an empty sentence).
func Test_SegmentWordStream_SpeakerChangeOnTerminal(t *testing.T) {
	words := get5Words()
	words[2].Word += "."
	for i := 3; i < 5; i++ {
		words[i].Speaker = 1
	}

	checkSplit(t, "terminal at speaker change", words, []int{2, 4})
}

// Known imperfection, accepted by docs/plan_llm_breakdown_on_demand.md:
// abbreviations and ordinals split. Pinned so the behaviour only changes on
// purpose.
func Test_SegmentWordStream_AbbreviationsSplit_KnownImperfection(t *testing.T) {
	words := getNWords(4)
	words[1].Word = "т.б."

	checkSplit(t, "abbreviation", words, []int{1, 3})
}

// The realistic shape: several sentences of different lengths in one stream,
// with every field checked end to end.
func Test_SegmentWordStream_MultipleSentences(t *testing.T) {
	words := []qarauv1.Word{
		{Word: "Мен", StartMs: 100, EndMs: 400, Confidence: 90},
		{Word: "мектепке", StartMs: 450, EndMs: 900, Confidence: 95},
		{Word: "барамын.", StartMs: 950, EndMs: 1400, Confidence: 99},
		{Word: "«Сен", StartMs: 1600, EndMs: 1900, Confidence: 80},
		{Word: "қайдасың?»", StartMs: 1950, EndMs: 2500, Confidence: 70},
		{Word: "Ол", StartMs: 2700, EndMs: 2900, Confidence: 88},
		{Word: "үйде", StartMs: 2950, EndMs: 3300, Confidence: 91},
	}

	res := checkSplit(t, "three sentences", words, []int{2, 4, 6})

	// The text fed to the LLM is the raw word text — no [word?] wrapping of
	// low-confidence words, per plan rule 4.
	if res[1].Text != "«Сен қайдасың?»" {
		t.Fatalf("got text %q instead of %q", res[1].Text, "«Сен қайдасың?»")
	}
}
