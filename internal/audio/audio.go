package audio

import (
	"fmt"
)

const (
	BytesPer16bitFrame       = 2
	I16ToF32ConversionFactor = 32768
	SampleRateKhz            = 16
	SampleRateHz             = SampleRateKhz * 1000
)

func makeI16OfBytes(lower byte, upper byte) int16 {
	return int16(lower) | (int16(upper) << 8)
}

func MakeF32OfBytes(lower byte, upper byte) float32 {
	i16 := makeI16OfBytes(lower, upper)
	f32 := float32(i16) / I16ToF32ConversionFactor
	return f32
}

func Convert16BitBytesToF32(pcm []byte) ([]float32, error) {
	frames := len(pcm) / BytesPer16bitFrame
	f32Data := make([]float32, frames)
	if err := Convert16BitBytesToF32WithBuffer(pcm, f32Data); err != nil {
		return nil, err
	}
	return f32Data, nil
}

func Convert16BitBytesToF32WithBuffer(pcm []byte, output []float32) error {
	frames := len(pcm) / BytesPer16bitFrame
	if frames*BytesPer16bitFrame != len(pcm) {
		return fmt.Errorf("incomplete frames in pcm: size %d", len(pcm))
	}
	if len(output) < frames {
		return fmt.Errorf("output buffer too small: %d < %d", len(output), frames)
	}
	for i := range frames {
		f32 := MakeF32OfBytes(pcm[i*2], pcm[i*2+1])
		output[i] = f32 // values are in the [-1.0, 1.0] range
	}
	return nil
}
