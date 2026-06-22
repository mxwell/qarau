package api

import (
	"fmt"
	"time"
)

/*
 * Information about video:
 * - to display to the user for visual confirmation
 * - to decide if it can be processed
 */
type Video struct {
	ID              int64
	OnlineVideoID   string
	Title           string
	ChannelID       string
	ChannelTitle    string
	PublishedAt     time.Time
	DurationSecs    int
	DefaultLang     string
	Embeddable      bool
	ThumbnailURL    string
	ThumbnailWidth  int32
	ThumbnailHeight int32
}

const secondsPer2H = 2 * 60 * 60

func (v *Video) ProcessingObstacle() string {
	if !v.Embeddable {
		return "the video can't be embedded"
	}
	if v.DefaultLang != "kk" {
		return "the video language is not Kazakh"
	}
	if v.DurationSecs > secondsPer2H {
		return fmt.Sprintf("the video is longer than %d seconds", secondsPer2H)
	}
	return ""
}
