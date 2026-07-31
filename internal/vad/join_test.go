package vad

import "testing"

func Test_JoinRanges_Single(t *testing.T) {
	singleRange := SegmentRange{
		RealStart: 5 * SampleRateKhz,
		RealEnd:   95 * SampleRateKhz,
		CopyStart: 5 * SampleRateKhz,
	}

	fragments := []Fragment{
		{
			Content: make([]byte, SampleRateKhz*500),
			Ranges:  []SegmentRange{singleRange},
		},
	}

	joined := JoinRanges(fragments)
	if len(joined) != 1 {
		t.Fatalf("joined result has %d items instead of 1", len(joined))
	}

	if joined[0] != singleRange {
		t.Fatalf("result range is %s instead of %s", joined[0].String(), singleRange.String())
	}
}

func Test_JoinRanges_Two(t *testing.T) {
	first := SegmentRange{
		RealStart: 5 * SampleRateKhz,
		RealEnd:   95 * SampleRateKhz,
		CopyStart: 5 * SampleRateKhz,
	}
	second := SegmentRange{
		RealStart: 505 * SampleRateKhz,
		RealEnd:   595 * SampleRateKhz,
		CopyStart: 5 * SampleRateKhz,
	}

	fragments := []Fragment{
		{
			Content: make([]byte, SampleRateKhz*BytesPer16bitFrame*500),
			Ranges:  []SegmentRange{first},
		},
		{
			Content: make([]byte, SampleRateKhz*BytesPer16bitFrame*500),
			Ranges:  []SegmentRange{second},
		},
	}

	joined := JoinRanges(fragments)
	if len(joined) != 2 {
		t.Fatalf("joined result has %d items instead of 2", len(joined))
	}

	expected := []SegmentRange{
		{5 * SampleRateKhz, 95 * SampleRateKhz, 5 * SampleRateKhz},
		{505 * SampleRateKhz, 595 * SampleRateKhz, 505 * SampleRateKhz},
	}

	if len(joined) != len(expected) {
		t.Fatalf("%d joined ranges instead of %d", len(joined), len(expected))
	}
	for i, j := range joined {
		if j != expected[i] {
			t.Fatalf("joind range #%d is %s instead of %s", i, j.String(), expected[i].String())
		}
	}
}
