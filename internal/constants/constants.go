package constants

import (
	"regexp"
	"time"
)

const (
	MaxDurationSecs         = 3 * 60 * 60 // 3 hours
	MinLikesCount           = 20
	MinViewsCount           = 1000
	AsrApiDailyQuotaSeconds = 10 * 60 * 60 // 10 hours per day
	PcmSampleRate           = 16_000

	MicrosecondsPerSecond = 1_000_000

	SentenceBatchSize = 25
)

var (
	KZ_TZ = time.FixedZone("KZ", 5*60*60)

	OnlineVideoIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	OnlinePlaylistIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{10,40}$`)
)
