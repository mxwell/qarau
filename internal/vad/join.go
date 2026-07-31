package vad

func JoinRanges(fragments []Fragment) []SegmentRange {
	size := 0
	for i := range fragments {
		size += len(fragments[i].Ranges)
	}
	result := make([]SegmentRange, 0, size)
	offset := 0
	for i := range fragments {
		for _, r := range fragments[i].Ranges {
			result = append(result, SegmentRange{
				RealStart: r.RealStart,
				RealEnd:   r.RealEnd,
				CopyStart: offset + r.CopyStart,
			})
		}
		offset += fragments[i].LengthInFrames()
	}
	return result
}
