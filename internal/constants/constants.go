package constants

import "time"

const (
	MaxDurationSecs         = 3 * 60 * 60 // 3 hours
	MinLikesCount           = 20
	MinViewsCount           = 1000
	AsrApiDailyQuotaSeconds = 10 * 60 * 60 // 10 hours per day
	PcmSampleRate           = 16_000
)

var (
	KZ_TZ = time.FixedZone("KZ", 5*60*60)
)
