package sentences

import (
	"fmt"
	"strconv"
	"testing"

	qarauv1 "github.com/mxwell/qarau/gen/qarau/v1"
)

func checkSentence(actual, expected Sentence, t *testing.T) {
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
		t.Fatalf("sent start word seq %d instead of %d", actual.StartWordSeq, expected.EndWordSeq)
	}
	if actual.EndWordSeq != expected.EndWordSeq {
		t.Fatalf("sent end word seq %d instead of %d", actual.EndWordSeq, expected.EndWordSeq)
	}
}

func Test_SegmentWordStream(t *testing.T) {
	res0 := SegmentWordStream([]qarauv1.Word{})
	if len(res0) != 0 {
		t.Fatalf("got %d sentences instead of 0 for empty input", len(res0))
	}

	w1 := qarauv1.Word{
		Word:       "w1",
		StartMs:    500,
		EndMs:      1000,
		Confidence: 100,
		Speaker:    0,
	}

	res1 := SegmentWordStream([]qarauv1.Word{w1})
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

	w2 := qarauv1.Word{
		Word:       "w2",
		StartMs:    1500,
		EndMs:      2000,
		Confidence: 100,
		Speaker:    0,
	}

	res2 := SegmentWordStream([]qarauv1.Word{w1, w2})
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
