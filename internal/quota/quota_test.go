package quota

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func checkDate(date pgtype.Date, year int, month time.Month, day int, t *testing.T) {
	if !date.Valid {
		t.Fatal("date must be Valid")
	}
	if date.Time.Year() != year {
		t.Fatalf("wrong year: %d instead of %d", date.Time.Year(), year)
	}
	if date.Time.Month() != month {
		t.Fatalf("wrong month: %d instead of %d", date.Time.Month(), month)
	}
	if date.Time.Day() != day {
		t.Fatalf("wrong day: %d instead of %d", date.Time.Day(), day)
	}
	if date.Time.Hour() != 0 || date.Time.Minute() != 0 || date.Time.Second() != 0 {
		t.Fatalf(
			"hours/minutes/seconds must be zero: %d/%d/%d",
			date.Time.Hour(),
			date.Time.Minute(),
			date.Time.Second(),
		)
	}
}

func Test_GetDayForQuota(t *testing.T) {
	same1 := GetDayForQuota(time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC))
	checkDate(same1, 2026, 7, 13, t)

	same2 := GetDayForQuota(time.Date(2026, 7, 13, 10, 11, 12, 0, time.UTC))
	checkDate(same2, 2026, 7, 13, t)

	same3 := GetDayForQuota(time.Date(2026, 7, 13, 18, 59, 59, 999, time.UTC))
	checkDate(same3, 2026, 7, 13, t)

	changed := GetDayForQuota(time.Date(2026, 7, 13, 19, 0, 0, 0, time.UTC))
	checkDate(changed, 2026, 7, 14, t)

	changed2 := GetDayForQuota(time.Date(2026, 7, 13, 23, 59, 59, 999, time.UTC))
	checkDate(changed2, 2026, 7, 14, t)
}

func Test_PrintQuotaDate(t *testing.T) {
	p := PrintQuotaDate(GetDayForQuota(time.Date(2009, 02, 01, 0, 0, 0, 0, time.UTC)))
	expected := "2009-02-01"
	if p != expected {
		t.Fatalf("printed date is %s instead of %s", p, expected)
	}
}
