package constants

import "time"

const (
	MaxDurationSecs         = 40 * 60 // let's start with a lower limit - 40 minutes
	MinLikesCount           = 20
	MinViewsCount           = 1000
	MaxViewsToLikesRatio    = 200          // refuse to process with less than 0.5% likes to views
	AsrApiDailyQuotaSeconds = 10 * 60 * 60 // 10 hours per day
)

var (
	KZ_TZ = time.FixedZone("KZ", 5*60*60)
)
