package api

import (
	"fmt"
	"time"

	constants "github.com/mxwell/qarau/internal/common"
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
	DurationSecs    int32
	Views           int64
	Likes           int64
	DefaultLang     string
	Embeddable      bool
	ThumbnailURL    string
	ThumbnailWidth  int32
	ThumbnailHeight int32
	LoadedFromDB    bool
}

func (v *Video) ProcessingObstacle() string {
	if !v.Embeddable {
		return "the video can't be embedded"
	}
	if v.DefaultLang != "kk" {
		return "the video language is not Kazakh"
	}
	if v.DurationSecs > constants.MaxDurationSecs {
		return fmt.Sprintf(
			"the video duration is too long: %d > %d seconds",
			v.DurationSecs,
			constants.MaxDurationSecs,
		)
	}
	if v.Likes < constants.MinLikesCount {
		return fmt.Sprintf(
			"the video has too few likes: %d < %d",
			v.Likes,
			constants.MinLikesCount,
		)
	}
	if v.Views < constants.MinViewsCount {
		return fmt.Sprintf(
			"the video has too few views: %d < %d",
			v.Views,
			constants.MinViewsCount,
		)
	}
	if v.Likes*constants.MaxViewsToLikesRatio < v.Views {
		return fmt.Sprintf(
			"%d - too few likes for a video with %d views",
			v.Likes,
			v.Views,
		)
	}
	return ""
}

type APIInfo struct {
	OnlineVideoID   string `json:"online_video_id"`
	Title           string `json:"title"`
	ChannelTitle    string `json:"channel_title"`
	DurationSecs    int32  `json:"duration_secs"`
	Views           int64  `json:"views"`
	Likes           int64  `json:"likes"`
	DefaultLang     string `json:"default_lang"`
	Embeddable      bool   `json:"embeddable"`
	ThumbnailURL    string `json:"thumbnail_url"`
	ThumbnailWidth  int32  `json:"thumbnail_width"`
	ThumbnailHeight int32  `json:"thumbnail_height"`
}

func NewAPIInfo(video *Video) APIInfo {
	return APIInfo{
		OnlineVideoID:   video.OnlineVideoID,
		Title:           video.Title,
		ChannelTitle:    video.ChannelTitle,
		DurationSecs:    video.DurationSecs,
		Views:           video.Views,
		Likes:           video.Likes,
		DefaultLang:     video.DefaultLang,
		Embeddable:      video.Embeddable,
		ThumbnailURL:    video.ThumbnailURL,
		ThumbnailWidth:  video.ThumbnailWidth,
		ThumbnailHeight: video.ThumbnailHeight,
	}
}
