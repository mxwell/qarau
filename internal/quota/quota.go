package quota

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	constants "github.com/mxwell/qarau/internal/common"
)

var (
	kz = time.FixedZone("KZ", 5*60*60)
)

func GetDayForQuota(t time.Time) pgtype.Date {
	kzTime := t.In(kz)
	return pgtype.Date{
		Time:  time.Date(kzTime.Year(), kzTime.Month(), kzTime.Day(), 0, 0, 0, 0, kz),
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
