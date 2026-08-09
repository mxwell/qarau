package api

import "github.com/mxwell/qarau/internal/constants"

func MicrosToFloorInt32Seconds(micros int64) int32 {
	return int32(micros / constants.MicrosecondsPerSecond)
}

func MicrosToCeilInt32Seconds(micros int64) int32 {
	numerator := micros + constants.MicrosecondsPerSecond - 1
	return MicrosToFloorInt32Seconds(numerator)
}
