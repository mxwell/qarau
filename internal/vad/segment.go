package vad

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/streamer45/silero-vad-go/speech"

	"github.com/mxwell/qarau/internal/audio"
)

var (
	ErrAudioNotMultipleOf16Bits = errors.New("audio length is not a multiple of 16 bits")

	BytesPer16bitFrame = audio.BytesPer16bitFrame
	SampleRateKhz      = audio.SampleRateKhz
)

/* Values are frame indices */
type SegmentRange struct {
	RealStart int // with padding
	RealEnd   int // with padding
	CopyStart int
}

func (r *SegmentRange) String() string {
	return fmt.Sprintf("{ %d-%d -> %d }", r.RealStart, r.RealEnd, r.CopyStart)
}

func (r *SegmentRange) Duration() int {
	return r.RealEnd - r.RealStart
}

type Fragment struct {
	Content       []byte
	Ranges        []SegmentRange
	RealSpeechEnd int // unit is frame indices, position is before padding
}

func (f *Fragment) reset() {
	f.Content = f.Content[:0]
	f.Ranges = f.Ranges[:0]
}

func (f *Fragment) LengthInFrames() int {
	return len(f.Content) / audio.BytesPer16bitFrame
}

func (f *Fragment) getRealEnd() int {
	n := len(f.Ranges)
	if n == 0 {
		return 0
	}
	return f.RealSpeechEnd
}

func (f *Fragment) appendSegment(content []byte, realStart, realSpeechEnd, realEnd int) {
	posInFrames := f.LengthInFrames()
	f.Content = append(f.Content, content...)
	f.Ranges = append(f.Ranges, SegmentRange{
		RealStart: realStart,
		RealEnd:   realEnd,
		CopyStart: posInFrames,
	})
	f.RealSpeechEnd = realSpeechEnd
}

func (f *Fragment) copy() Fragment {
	return Fragment{
		Content:       slices.Clone(f.Content),
		Ranges:        slices.Clone(f.Ranges),
		RealSpeechEnd: f.RealSpeechEnd,
	}
}

type SegmentationParams struct {
	FragmentMaxFrames int // we stop adding content to a fragment once it reaches this number of frames
	GapMaxFrames      int // we finalize a current fragment if we meet a gap longer than this number of frames
}

type collector struct {
	params    SegmentationParams
	fragments []Fragment
	current   Fragment
}

func newCollector(params SegmentationParams) *collector {
	return &collector{
		params: params,
	}
}

func (c *collector) finalize() {
	c.fragments = append(c.fragments, c.current.copy())

	c.current.reset()
}

func (c *collector) finalizeIfMaxFrames() {
	if c.current.LengthInFrames() >= c.params.FragmentMaxFrames {
		//log.Printf("finalize after max frames: %d", c.current.LengthInFrames())
		c.finalize()
	}
}

func (c *collector) finalizeIfGap(nextSpeechStartInFrames int) {
	gap := nextSpeechStartInFrames - c.current.getRealEnd()
	if gap >= c.params.GapMaxFrames {
		//log.Printf("finalize before gap: %d", gap)
		c.finalize()
	}
}

func (c *collector) finalizeIfPresent() {
	if c.current.LengthInFrames() == 0 {
		return
	}
	c.finalize()
}

func (c *collector) doAppendSegment(paddedSegment []byte, startInFrames, speechEndInFrames, endInFrames int) {
	c.current.appendSegment(paddedSegment, startInFrames, speechEndInFrames, endInFrames)

	c.finalizeIfMaxFrames()
}

func (c *collector) appendSegment(paddedSegment []byte, speechStartInFrames, startInFrames, speechEndInFrames, endInFrames int) {
	if c.current.LengthInFrames() != 0 {
		c.finalizeIfGap(speechStartInFrames)
	}
	c.doAppendSegment(paddedSegment, startInFrames, speechEndInFrames, endInFrames)
}

func float64SecondsToMillis(seconds float64) int {
	return int(math.Round(seconds * 1000))
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

/**
 * The audio must be:
 * - 16 bit: 1 frame takes 2 byte positions
 * - 16 kHz: 16000 frames per second or 16 frames per millisecond
 */
func SegmentAudioByTimestampRanges(
	log *slog.Logger,
	pcm []byte,
	timestampRanges []speech.Segment,
	padMillis int,
	gapMaxMillis int,
	fragmentMaxMillis int,
) ([]Fragment, error) {
	totalFrames := len(pcm) / BytesPer16bitFrame
	if len(pcm) != totalFrames*BytesPer16bitFrame {
		return nil, ErrAudioNotMultipleOf16Bits
	}
	totalMillis := ceilDiv(totalFrames, SampleRateKhz)

	gapMaxFrames := gapMaxMillis * SampleRateKhz
	fragmentMaxFrames := fragmentMaxMillis * SampleRateKhz

	c := newCollector(SegmentationParams{
		FragmentMaxFrames: fragmentMaxFrames,
		GapMaxFrames:      gapMaxFrames,
	})

	n := len(timestampRanges)
	prevEndMillis := 0

	for i := range timestampRanges {
		startMillis := float64SecondsToMillis(timestampRanges[i].SpeechStartAt)
		speechStartInFrames := startMillis * SampleRateKhz
		if speechStartInFrames < 0 || speechStartInFrames >= totalFrames {
			log.Error(
				"segment start timestamp out of range",
				"func", "SegmentAudioByTimestampRanges",
				"i", i,
				"speechStartInFrames", speechStartInFrames,
				"totalFrames", totalFrames,
				"SpeechEndAt", timestampRanges[i].SpeechEndAt,
				"timestampRanges", len(timestampRanges),
			)
			continue
		}

		endMillis := float64SecondsToMillis(timestampRanges[i].SpeechEndAt)
		speechEndInFrames := endMillis * SampleRateKhz
		if speechEndInFrames <= speechStartInFrames {
			log.Error(
				"segment end timestamp out of range",
				"func", "SegmentAudioByTimestampRanges",
				"i", i,
				"speechStartInFrames", speechStartInFrames,
				"speechEndInFrames", speechEndInFrames,
				"totalFrames", totalFrames,
				"SpeechEndAt", timestampRanges[i].SpeechEndAt,
				"timestampRanges", len(timestampRanges),
			)
			continue
		}

		prevGap := startMillis - prevEndMillis
		var takeFromPrevGap int
		if prevGap <= padMillis*2 {
			if i == 0 {
				// take whole gap if it's the first segment
				takeFromPrevGap = min(prevGap, padMillis)
			} else {
				takeFromPrevGap = prevGap - prevGap/2 // next segment takes half+0.5 if odd
			}
		} else {
			takeFromPrevGap = padMillis
		}

		paddedStartMillis := startMillis - takeFromPrevGap

		nextMillis := totalMillis
		if i+1 < n {
			nextMillis = float64SecondsToMillis(timestampRanges[i+1].SpeechStartAt)
		}

		nextGap := nextMillis - endMillis
		var takeFromNextGap int
		if nextGap <= padMillis*2 {
			if i+1 < n {
				takeFromNextGap = nextGap / 2 // prev segment takes half-0.5 if odd
			} else {
				// take whole gap if it's the last segment
				takeFromNextGap = min(nextGap, padMillis)
			}
		} else {
			takeFromNextGap = padMillis
		}

		paddedEndMillis := endMillis + takeFromNextGap

		paddedStartInFrames := paddedStartMillis * SampleRateKhz
		paddedEndInFrames := min(totalFrames, paddedEndMillis*SampleRateKhz)

		//log.Printf("startMillis %d\n", startMillis)
		//log.Printf("taking range: %d -> %d\n", paddedStartInFrames, paddedEndInFrames)

		c.appendSegment(
			pcm[paddedStartInFrames*BytesPer16bitFrame:paddedEndInFrames*BytesPer16bitFrame],
			speechStartInFrames,
			paddedStartInFrames,
			speechEndInFrames,
			paddedEndInFrames,
		)

		prevEndMillis = endMillis
	}

	c.finalizeIfPresent()

	return c.fragments, nil
}

func StitchAsFloat32(fragments []Fragment, cutAtFrames int) ([]float32, error) {
	result := make([]float32, 0)
	for _, f := range fragments {
		f32Data, err := audio.Convert16BitBytesToF32(f.Content)
		if err != nil {
			return nil, err
		}
		take := min(len(f32Data), cutAtFrames-len(result))
		result = append(result, f32Data[:take]...)
		if len(result) >= cutAtFrames {
			break
		}
	}
	return result, nil
}
