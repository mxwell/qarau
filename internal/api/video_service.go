package api

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/mxwell/qarau/db/gen"
)

var (
	ErrCorruptedData = errors.New("corrupted data")
)

type VideoService struct {
	log      *slog.Logger
	queries  *dbgen.Queries
	ytClient *YtClient
}

func NewVideoService(log *slog.Logger, queries *dbgen.Queries, ytClient *YtClient) (*VideoService, error) {
	if log == nil {
		return nil, errors.New("nil logger in VideoService creation")
	}
	if queries == nil {
		return nil, errors.New("nil queries in VideoService creation")
	}
	if ytClient == nil {
		return nil, errors.New("nil YT client in VideoService creation")
	}
	return &VideoService{
		log:      log,
		queries:  queries,
		ytClient: ytClient,
	}, nil
}

const microsecondsPerSecond = 1_000_000

func rowToVideo(row *dbgen.GetVideoRow) (*Video, error) {
	if row.DefaultLang == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailUrl == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailWidth == nil {
		return nil, ErrCorruptedData
	}
	if row.ThumbnailHeight == nil {
		return nil, ErrCorruptedData
	}
	return &Video{
		ID:              row.ID,
		OnlineVideoID:   row.OnlineVideoID,
		Title:           row.Title,
		ChannelID:       row.ChannelID,
		ChannelTitle:    row.ChannelTitle,
		PublishedAt:     row.PublishedAt.Time,
		DurationSecs:    int(row.Duration.Microseconds / microsecondsPerSecond),
		DefaultLang:     *row.DefaultLang,
		Embeddable:      row.Embeddable,
		ThumbnailURL:    *row.ThumbnailUrl,
		ThumbnailWidth:  *row.ThumbnailWidth,
		ThumbnailHeight: *row.ThumbnailHeight,
	}, nil
}

/*
 * The function returns video metadata if it's available. Also, the metadata is stored to DB.
 */
func (s *VideoService) ProbeVideo(ctx context.Context, onlineVideoId string) (*Video, error) {
	existing, err := s.queries.GetVideo(ctx, onlineVideoId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("failed to check video in DB", "onlineVideoId", onlineVideoId, "err", err)
			return nil, err
		}
	} else {
		s.log.Info("loaded video from DB", "onlineVideoId", onlineVideoId, "id", existing.ID)
		dbVideo, convErr := rowToVideo(&existing)
		if convErr != nil {
			s.log.Error("failed to convert loaded video", "onlineVideoId", onlineVideoId, "err", convErr)
			return nil, convErr
		}
		return dbVideo, nil
	}
	video, err := s.ytClient.GetVideoInformation(ctx, onlineVideoId)
	if err != nil {
		return nil, err
	}
	s.log.Info("loaded video information from external API", "onlineVideoId", onlineVideoId, "title", video.Title, "lang", video.DefaultLang)

	duration := pgtype.Interval{
		Microseconds: int64(video.DurationSecs) * microsecondsPerSecond,
		Valid:        true,
	}

	// XXX one of concurrent inserts will fail with a conflict (online_video_id is UNIQUE)
	videoID, err := s.queries.CreateVideo(ctx, dbgen.CreateVideoParams{
		OnlineVideoID:   video.OnlineVideoID,
		Title:           video.Title,
		ChannelID:       video.ChannelID,
		ChannelTitle:    video.ChannelTitle,
		PublishedAt:     pgtype.Timestamptz{Time: video.PublishedAt, Valid: true},
		Duration:        duration,
		DefaultLang:     &video.DefaultLang,
		Embeddable:      video.Embeddable,
		ThumbnailUrl:    &video.ThumbnailURL,
		ThumbnailWidth:  &video.ThumbnailWidth,
		ThumbnailHeight: &video.ThumbnailHeight,
	})
	if err != nil {
		s.log.Error("failed to store loaded to DB", "onlineVideoId", onlineVideoId, "err", err)
		return nil, err
	}
	s.log.Info("stored video to DB", "onlineVideoId", onlineVideoId, "ID", videoID)
	video.ID = videoID
	return video, nil
}

func (s *VideoService) CreateFetchJob(ctx context.Context, videoID int64) (jobID int64, err error) {
	// TODO
	return 0, nil
}
