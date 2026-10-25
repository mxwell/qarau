package evalset

type Batch struct {
	VideoID       int64    `json:"video_id"`
	OnlineVideoID string   `json:"online_video_id"`
	VideoTitle    string   `json:"video_title"`
	ChannelTitle  string   `json:"channel_title"`
	StartSentSeq  int32    `json:"start_sent_seq"`
	Sentences     []string `json:"sentences"`
}

type Set struct {
	Batches []Batch `json:"batches"`
}
