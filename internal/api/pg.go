package api

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mxwell/qarau/internal/constants"
)

func NewTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  t,
		Valid: true,
	}
}

func NewInterval(seconds int64) pgtype.Interval {
	return pgtype.Interval{
		Microseconds: seconds * constants.MicrosecondsPerSecond,
		Valid:        true,
	}
}
