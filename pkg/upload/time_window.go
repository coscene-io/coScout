// Copyright 2026 coScene
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

package upload

import (
	"errors"
	"fmt"
	"time"
)

const (
	// FutureStartTolerance permits ordinary clock skew and short scheduling
	// jitter without postponing an otherwise runnable scan.
	FutureStartTolerance = 5 * time.Minute
)

var (
	// ErrInvalidTimeWindow identifies a permanent request error.
	ErrInvalidTimeWindow = errors.New("invalid upload time window")
	// ErrTimeWindowNotReady classifies a request whose start time is beyond the
	// accepted future tolerance. Scan entry points translate it to empty success.
	ErrTimeWindowNotReady = errors.New("upload time window is not ready")
)

// TimeWindowError includes the timestamps that caused validation to fail and
// unwraps to either ErrInvalidTimeWindow or ErrTimeWindowNotReady.
type TimeWindowError struct {
	StartTime int64
	EndTime   int64
	Now       int64
	kind      error
}

func (e *TimeWindowError) Error() string {
	switch {
	case errors.Is(e.kind, ErrInvalidTimeWindow):
		return fmt.Sprintf("%v: start %d is after end %d", e.kind, e.StartTime, e.EndTime)
	case errors.Is(e.kind, ErrTimeWindowNotReady):
		return fmt.Sprintf(
			"%v: start %d is after current time %d plus tolerance %s",
			e.kind,
			e.StartTime,
			e.Now,
			FutureStartTolerance,
		)
	default:
		return fmt.Sprintf("upload time window validation failed: start=%d end=%d now=%d", e.StartTime, e.EndTime, e.Now)
	}
}

func (e *TimeWindowError) Unwrap() error {
	return e.kind
}

// ValidateTimeWindow rejects permanently invalid ranges and classifies ranges
// whose start time is beyond the accepted tolerance. A future end time is valid.
func ValidateTimeWindow(startTime, endTime int64) error {
	return validateTimeWindowAt(startTime, endTime, time.Now())
}

func validateTimeWindowAt(startTime, endTime int64, now time.Time) error {
	nowUnix := now.Unix()
	if startTime > endTime {
		return &TimeWindowError{
			StartTime: startTime,
			EndTime:   endTime,
			Now:       nowUnix,
			kind:      ErrInvalidTimeWindow,
		}
	}
	if startTime > now.Add(FutureStartTolerance).Unix() {
		return &TimeWindowError{
			StartTime: startTime,
			EndTime:   endTime,
			Now:       nowUnix,
			kind:      ErrTimeWindowNotReady,
		}
	}
	return nil
}
