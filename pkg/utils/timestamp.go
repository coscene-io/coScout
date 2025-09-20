// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"math"
	"time"
)

// NormalizeFloatTimestamp normalizes a floating-point timestamp to seconds and nanoseconds.
// keep the precision of the timestamp with microseconds precision.
func NormalizeFloatTimestamp(timestamp float64) (int64, int32) {
	var sec float64
	var nsec float64

	switch {
	case timestamp >= 1e18: // nanoseconds
		sec = timestamp / 1e9
		nsec = math.Mod(timestamp, 1e9)
	case timestamp >= 1e15: // microseconds
		sec = timestamp / 1e6
		nsec = math.Mod(timestamp, 1e6) * 1e3
	case timestamp >= 1e12: // milliseconds
		sec = timestamp / 1e3
		nsec = math.Mod(timestamp, 1e3) * 1e6
	default: // seconds
		sec, nsec = math.Modf(timestamp)
		nsec *= 1e9
	}

	return int64(sec), int32(nsec)
}

// TimeFromFloat converts a floating-point timestamp to a time.Time instance.
func TimeFromFloat(timestamp float64) time.Time {
	sec, nsec := NormalizeFloatTimestamp(timestamp)
	return time.Unix(sec, int64(nsec))
}

// FloatSecFromTime converts a time.Time instance to a floating-point timestamp in seconds.
func FloatSecFromTime(t time.Time) float64 {
	return float64(t.UnixNano()) / 1_000_000_000
}
