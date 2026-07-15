package constants

import "time"

const (
	MaxDurationSecs         = 40 * 60 // let's start with a lower limit - 40 minutes
	MinLikesCount           = 20
	MinViewsCount           = 1000
	AsrApiDailyQuotaSeconds = 10 * 60 * 60 // 10 hours per day
	PcmSampleRate           = 16_000
)

var (
	KZ_TZ = time.FixedZone("KZ", 5*60*60)
)
