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
	"time"
)

// NormalizeFloatTimestamp normalizes a floating-point timestamp to seconds and nanoseconds.
// keep the precision of the timestamp with microseconds precision.
func NormalizeFloatTimestamp(timestamp float64) (int64, int32) {
	switch {
	case timestamp >= 1_000_000_000_000_000_000:
		// timestamp in nanoseconds
		sec := int64(timestamp / 1_000_000_000)
		nsec := (int64(timestamp/1000) - sec*1_000_000) * 1_000
		//nolint: gosec // ignore int64 to int32 conversion
		return sec, int32(nsec)
	case timestamp >= 1_000_000_000_000_000:
		// timestamp in microseconds
		sec := int64(timestamp / 1_000_000)
		nsec := (int64(timestamp) - sec*1_000_000) * 1_000
		//nolint: gosec // ignore int64 to int32 conversion
		return sec, int32(nsec)
	case timestamp >= 1_000_000_000_000:
		// timestamp in milliseconds
		sec := int64(timestamp / 1_000)
		nsec := (int64(timestamp*1_000) - sec*1_000_000) * 1_000
		//nolint: gosec // ignore int64 to int32 conversion
		return sec, int32(nsec)
	default:
		// timestamp in seconds, with microseconds precision
		sec := int64(timestamp)
		nsec := (int64(timestamp*1000_000) - sec*1_000_000) * 1_000
		//nolint: gosec // ignore int64 to int32 conversion
		return sec, int32(nsec)
	}
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
