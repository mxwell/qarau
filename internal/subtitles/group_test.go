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
					Words: []OutputWord{
						{"w1", 505, 805},
						{"w2", 905, 1205},
					},
					StartMs: 505,
					EndMs:   1205,
				},
				{
					Words: []OutputWord{
						{"w3", 1405, 1705},
						{"w4", 1805, 2105},
					},
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
					Words: []OutputWord{
						{"w1", 505, 805},
						{"w2", 905, 1205},
						{"w3", 1405, 1705},
					},
					StartMs: 505,
					EndMs:   1705,
				},
				{
					Words: []OutputWord{
						{"w4", 1805, 2105},
					},
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
					Words: []OutputWord{
						{"w1", 505, 805},
						{"w2", 905, 1205},
					},
					StartMs: 505,
					EndMs:   1205,
				},
				{
					Words: []OutputWord{
						{"w3", 1405, 1705},
						{"w4", 1805, 2105},
					},
					StartMs: 1405,
					EndMs:   2105,
				},
				{
					Words: []OutputWord{
						{"w5", 2405, 2705},
						{"w6", 2805, 3105},
						{"w7", 3205, 3405},
					},
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
					Words: []OutputWord{
						{"w1", 0, 100},
					},
					StartMs: 0,
					EndMs:   100,
				},
				{
					Words: []OutputWord{
						{"w2", 201, 300},
						{"w3", 400, 500},
					},
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
					Words: []OutputWord{
						{"w1", 505, 805},
						{"w2", 905, 1205},
						{"w3", 1405, 1705},
					},
					StartMs: 505,
					EndMs:   1705,
				},
				{
					Words: []OutputWord{
						{"w4", 1805, 2105},
						{"w5", 2405, 2705},
						{"w6", 2805, 3105},
					},
					StartMs: 1805,
					EndMs:   3105,
				},
				{
					Words: []OutputWord{
						{"w7", 3205, 3405},
					},
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
				{[]OutputWord{{"w1", 0, 100}}, 0, 100},
			},
		},
		{
			"allWords",
			slices.Concat(words4, words2),
			1000,
			100,
			[]Subtitle{
				{
					Words: []OutputWord{
						{"w1", 505, 805},
						{"w2", 905, 1205},
						{"w3", 1405, 1705},
						{"w4", 1805, 2105},
						{"w5", 2405, 2705},
						{"w6", 2805, 3105},
						{"w7", 3205, 3405},
					},
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
				{[]OutputWord{{"w1", 505, 805}}, 505, 805},
				{[]OutputWord{{"w2", 905, 1205}}, 905, 1205},
				{[]OutputWord{{"w3", 1405, 1705}}, 1405, 1705},
				{[]OutputWord{{"w4", 1805, 2105}}, 1805, 2105},
				{[]OutputWord{{"w5", 2405, 2705}}, 2405, 2705},
				{[]OutputWord{{"w6", 2805, 3105}}, 2805, 3105},
				{[]OutputWord{{"w7", 3205, 3405}}, 3205, 3405},
			},
		},
		{
			"overlap",
			[]InputWord{
				{"w1", 0, 101, 0},
				{"w2", 100, 301, 0},
				{"w3", 300, 500, 0},
				{"w4", 601, 700, 0},
			},
			100,
			10,
			[]Subtitle{
				{
					Words: []OutputWord{
						{"w1", 0, 101},
						{"w2", 100, 301},
						{"w3", 300, 500},
					},
					StartMs: 0,
					EndMs:   500,
				},
				{
					Words: []OutputWord{
						{"w4", 601, 700},
					},
					StartMs: 601,
					EndMs:   700,
				},
			},
		},
		{
			"2speakers",
			[]InputWord{
				{"w1", 0, 100, 0},
				{"w2", 101, 200, 0},
				{"w3", 201, 300, 1},
				{"w4", 301, 400, 0},
			},
			100,
			10,
			[]Subtitle{
				{
					Words: []OutputWord{
						{"w1", 0, 100},
						{"w2", 101, 200},
					},
					StartMs: 0,
					EndMs:   200,
				},
				{
					Words: []OutputWord{
						{"w3", 201, 300},
					},
					StartMs: 201,
					EndMs:   300,
				},
				{
					Words: []OutputWord{
						{"w4", 301, 400},
					},
					StartMs: 301,
					EndMs:   400,
				},
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
