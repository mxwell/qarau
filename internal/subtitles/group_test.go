package subtitles

import (
	"reflect"
	"slices"
	"testing"
)

func checkSize(subtitles []Subtitle, expected int, t *testing.T) {
	if len(subtitles) != expected {
		t.Fatalf("got %d subtitles, want %d", len(subtitles), expected)
	}
}

func Test_Group(t *testing.T) {
	words4 := []InputWord{
		{
			Word:    "w1",
			StartMs: 505,
			EndMs:   805,
		},
		{
			Word:    "w2",
			StartMs: 905,
			EndMs:   1205,
		},
		{
			Word:    "w3",
			StartMs: 1405,
			EndMs:   1705,
		},
		{
			Word:    "w4",
			StartMs: 1805,
			EndMs:   2105,
		},
	}
	words2 := []InputWord{
		{
			Word:    "w5",
			StartMs: 2405,
			EndMs:   2705,
		},
		{
			Word:    "w6",
			StartMs: 2805,
			EndMs:   3105,
		},
		{
			Word:    "w7",
			StartMs: 3205,
			EndMs:   3405,
		},
	}

	data := []struct {
		name       string
		words      []InputWord
		maxPauseMs int
		maxWords   int
		expected   []Subtitle
	}{
		{
			name:       "res1",
			words:      words4,
			maxPauseMs: 100,
			maxWords:   5,
			expected: []Subtitle{
				{
					Text:    "w1 w2",
					StartMs: 505,
					EndMs:   1205,
				},
				{
					Text:    "w3 w4",
					StartMs: 1405,
					EndMs:   2105,
				},
			},
		},
		{
			"res2",
			words4,
			200,
			3,
			[]Subtitle{
				{
					Text:    "w1 w2 w3",
					StartMs: 505,
					EndMs:   1705,
				},
				{
					Text:    "w4",
					StartMs: 1805,
					EndMs:   2105,
				},
			},
		},
		{
			"res3",
			[]InputWord{},
			100,
			10,
			[]Subtitle{},
		},
		{
			"res4",
			slices.Concat(words4, words2),
			100,
			3,
			[]Subtitle{
				{
					Text:    "w1 w2",
					StartMs: 505,
					EndMs:   1205,
				},
				{
					Text:    "w3 w4",
					StartMs: 1405,
					EndMs:   2105,
				},
				{
					Text:    "w5 w6 w7",
					StartMs: 2405,
					EndMs:   3405,
				},
			},
		},
		{
			"maxPause",
			[]InputWord{
				{
					Word:    "w1",
					StartMs: 0,
					EndMs:   100,
				},
				{
					Word:    "w2",
					StartMs: 201,
					EndMs:   300,
				},
				{
					Word:    "w3",
					StartMs: 400,
					EndMs:   500,
				},
			},
			100,
			100,
			[]Subtitle{
				{
					Text:    "w1",
					StartMs: 0,
					EndMs:   100,
				},
				{
					Text:    "w2 w3",
					StartMs: 201,
					EndMs:   500,
				},
			},
		},
		{
			"maxWords",
			slices.Concat(words4, words2),
			1000,
			3,
			[]Subtitle{
				{
					Text:    "w1 w2 w3",
					StartMs: 505,
					EndMs:   1705,
				},
				{
					Text:    "w4 w5 w6",
					StartMs: 1805,
					EndMs:   3105,
				},
				{
					Text:    "w7",
					StartMs: 3205,
					EndMs:   3405,
				},
			},
		},
		{
			"singleWord",
			[]InputWord{
				{
					Word:    "w1",
					StartMs: 0,
					EndMs:   100,
				},
			},
			1000,
			100,
			[]Subtitle{
				{"w1", 0, 100},
			},
		},
		{
			"allWords",
			slices.Concat(words4, words2),
			1000,
			100,
			[]Subtitle{
				{
					Text:    "w1 w2 w3 w4 w5 w6 w7",
					StartMs: 505,
					EndMs:   3405,
				},
			},
		},
		{
			"splitAll",
			slices.Concat(words4, words2),
			10000,
			1,
			[]Subtitle{
				{"w1", 505, 805},
				{"w2", 905, 1205},
				{"w3", 1405, 1705},
				{"w4", 1805, 2105},
				{"w5", 2405, 2705},
				{"w6", 2805, 3105},
				{"w7", 3205, 3405},
			},
		},
		{
			"overlap",
			[]InputWord{
				{"w1", 0, 101},
				{"w2", 100, 301},
				{"w3", 300, 500},
				{"w4", 601, 700},
			},
			100,
			10,
			[]Subtitle{
				{"w1 w2 w3", 0, 500},
				{"w4", 601, 700},
			},
		},
	}

	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			res := Group(d.words, d.maxPauseMs, d.maxWords)
			checkSize(res, len(d.expected), t)
			for i := range res {
				if !reflect.DeepEqual(res[i], d.expected[i]) {
					t.Fatalf("%s[%d]: subtitle diff - %v instead of %v", d.name, i, res[i], d.expected[i])
				}
			}
		})
	}
}
