package api

type Playlist struct {
	OnlinePlaylistID string `json:"online_playlist_id"`
	Title            string `json:"title"`
	ThumbnailURL     string `json:"thumbnail_url"`
	ThumbnailWidth   int32  `json:"thumbnail_width"`
	ThumbnailHeight  int32  `json:"thumbnail_height"`
	ItemCount        int32  `json:"item_count"`
}
