package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"google.golang.org/api/option"
	yt "google.golang.org/api/youtube/v3"
)

var (
	ErrYtApiFail         = errors.New("YouTube API failure")
	ErrResponseParseFail = errors.New("YouTube API response parse failure")
	ErrNoSuchVideo       = errors.New("no such video")
)

type YtClient struct {
	log *slog.Logger
	svc *yt.Service
}

func NewYtClient(ctx context.Context, log *slog.Logger, apiKey string) (*YtClient, error) {
	svc, err := yt.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}
	return &YtClient{
		log: log,
		svc: svc,
	}, nil
}

var durationRE = regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)

func parseDurationSecs(iso string) int64 {
	m := durationRE.FindStringSubmatch(iso)
	if m == nil {
		return 0
	}
	parse := func(s string) int64 {
		if s == "" {
			return 0
		}
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	return parse(m[1])*3600 + parse(m[2])*60 + parse(m[3])
}

func (c *YtClient) pickThumbnail(thumbnails *yt.ThumbnailDetails) (*yt.Thumbnail, error) {
	if thumbnails != nil {
		// Start with something medium sized, then proceed with MaxRes as the 2nd choice
		variants := []*yt.Thumbnail{
			thumbnails.High,
			thumbnails.Maxres,
			thumbnails.Medium,
			thumbnails.Standard,
			thumbnails.Default,
		}
		for _, t := range variants {
			if t != nil {
				return t, nil
			}
		}
	}
	c.log.Error("failed to pick thumbnail", "thumbnails", thumbnails)
	return nil, ErrResponseParseFail
}

func (c *YtClient) GetVideoInformation(ctx context.Context, onlineVideoID string) (*Video, error) {
	response, err := c.svc.Videos.
		List([]string{"snippet", "contentDetails", "status"}).
		Id(onlineVideoID).
		Context(ctx).
		Do()
	if err != nil {
		c.log.Error("failed to fetch from YouTube API", "err", err)
		return nil, fmt.Errorf("%w: %w", ErrYtApiFail, err)
	}

	for _, v := range response.Items {
		if v.Id != onlineVideoID {
			c.log.Info("skipping fetched video with different ID", "onlineVideoID", onlineVideoID, "loaded_id", v.Id)
			continue
		}

		publishedAt, err := time.Parse(time.RFC3339, v.Snippet.PublishedAt)
		if err != nil {
			c.log.Error(
				"failed to parse PublishedAt",
				"err", err,
				"v.Snippet.PublishedAt", v.Snippet.PublishedAt,
			)
			return nil, ErrResponseParseFail
		}

		durationSecs := parseDurationSecs(v.ContentDetails.Duration)
		if durationSecs == 0 {
			c.log.Error("failed to parse video duration", "v.ContentDetails.Duration", v.ContentDetails.Duration)
			return nil, ErrResponseParseFail
		}

		thumbnail, err := c.pickThumbnail(v.Snippet.Thumbnails)
		if err != nil {
			return nil, err
		}

		return &Video{
			ID:              0,
			OnlineVideoID:   onlineVideoID,
			Title:           v.Snippet.Title,
			ChannelID:       v.Snippet.ChannelId,
			ChannelTitle:    v.Snippet.ChannelTitle,
			PublishedAt:     publishedAt,
			DurationSecs:    int(durationSecs),
			DefaultLang:     v.Snippet.DefaultLanguage,
			Embeddable:      v.Status.Embeddable,
			ThumbnailURL:    thumbnail.Url,
			ThumbnailWidth:  int32(thumbnail.Width),
			ThumbnailHeight: int32(thumbnail.Height),
		}, nil
	}

	return nil, ErrNoSuchVideo
}
