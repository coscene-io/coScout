package utils

import (
	"time"
)

// NormalizeFloatTimestamp normalizes a floating-point timestamp to seconds and nanoseconds.
func NormalizeFloatTimestamp(timestamp float64) (int64, int64) {
	switch {
	case timestamp >= 1_000_000_000_000_000_000:
		// timestamp in nanoseconds
		sec := int64(timestamp / 1_000_000_000)
		nsec := int64(timestamp) % 1_000_000_000
		return sec, nsec
	case timestamp >= 1_000_000_000_000_000:
		// timestamp in microseconds
		sec := int64(timestamp / 1_000_000)
		nsec := int64(timestamp) % 1_000_000 * 1_000
		return sec, nsec
	case timestamp >= 1_000_000_000_000:
		// timestamp in milliseconds
		sec := int64(timestamp / 1_000)
		nsec := int64(timestamp) % 1_000 * 1_000_000
		return sec, nsec
	default:
		// timestamp in seconds
		return int64(timestamp), 0
	}
}

// TimeFromFloat converts a floating-point timestamp to a time.Time instance.
func TimeFromFloat(timestamp float64) time.Time {
	sec, nsec := NormalizeFloatTimestamp(timestamp)
	return time.Unix(sec, nsec)
}

// FloatSecFromTime converts a time.Time instance to a floating-point timestamp in seconds.
func FloatSecFromTime(t time.Time) float64 {
	return float64(t.UnixNano()) / 1_000_000_000
}
