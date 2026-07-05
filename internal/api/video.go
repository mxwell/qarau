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
	LoadedFromDB    bool
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

type APIInfo struct {
	OnlineVideoID string `json:"online_video_id"`
	Title         string `json:"title"`
	ChannelTitle  string `json:"channel_title"`
	DurationSecs  int    `json:"duration_secs"`
	DefaultLang   string `json:"default_lang"`
	Embeddable    bool   `json:"embeddable"`
}

func NewAPIInfo(video *Video) APIInfo {
	return APIInfo{
		OnlineVideoID: video.OnlineVideoID,
		Title:         video.Title,
		ChannelTitle:  video.ChannelTitle,
		DurationSecs:  video.DurationSecs,
		DefaultLang:   video.DefaultLang,
		Embeddable:    video.Embeddable,
	}
}
