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
	"testing"
	"time"
)

func TestValidateTimeWindow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	tests := []struct {
		name      string
		startTime int64
		endTime   int64
		wantErr   error
	}{
		{
			name:      "invalid order is permanent",
			startTime: now.Unix() + 1,
			endTime:   now.Unix(),
			wantErr:   ErrInvalidTimeWindow,
		},
		{
			name:      "start at tolerance boundary is runnable",
			startTime: now.Add(FutureStartTolerance).Unix(),
			endTime:   now.Add(FutureStartTolerance + time.Minute).Unix(),
		},
		{
			name:      "start beyond tolerance is classified for empty success",
			startTime: now.Add(FutureStartTolerance + time.Second).Unix(),
			endTime:   now.Add(FutureStartTolerance + time.Minute).Unix(),
			wantErr:   ErrTimeWindowNotReady,
		},
		{
			name:      "future end is runnable",
			startTime: now.Add(-time.Minute).Unix(),
			endTime:   now.Add(24 * time.Hour).Unix(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateTimeWindowAt(tc.startTime, tc.endTime, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
