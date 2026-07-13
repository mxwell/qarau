package quota

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mxwell/qarau/internal/constants"
)

func GetDayForQuota(t time.Time) pgtype.Date {
	kzTime := t.In(constants.KZ_TZ)
	return pgtype.Date{
		Time: time.Date(
			kzTime.Year(),
			kzTime.Month(),
			kzTime.Day(),
			0,
			0,
			0,
			0,
			constants.KZ_TZ,
		),
		Valid: true,
	}
}

func GetTodayForQuota() pgtype.Date {
	return GetDayForQuota(time.Now())
}

func PrintQuotaDate(d pgtype.Date) string {
	return d.Time.Format(time.DateOnly)
}

func CalculateUsedPercent(usedSeconds int32) int16 {
	if usedSeconds < 0 {
		return 0
	}
	if usedSeconds >= constants.AsrApiDailyQuotaSeconds {
		return 100
	}
	return int16(usedSeconds * 100 / constants.AsrApiDailyQuotaSeconds)
}
