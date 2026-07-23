package subtitles

import (
	"fmt"
	"strconv"
	"strings"
)

func formatTimestamp(totalMillis int) string {
	totalSecs := totalMillis / 1000
	millis := totalMillis - totalSecs*1000
	h := totalSecs / 3600
	remainderSecs := totalSecs - h*3600
	m := remainderSecs / 60
	s := remainderSecs - m*60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, millis)
}

func ConvertFrom(subtitles []Subtitle) string {
	if len(subtitles) == 0 {
		return ""
	}
	lines := make([]string, 0, len(subtitles)*4-1)
	for i, sub := range subtitles {
		if i > 0 {
			lines = append(lines, "")
		}
		start := formatTimestamp(sub.StartMs)
		end := formatTimestamp(sub.EndMs)
		lines = append(
			lines,
			strconv.Itoa(i+1),
			start+" --> "+end,
			sub.JoinText(),
		)
	}
	return strings.Join(lines, "\n")
}
