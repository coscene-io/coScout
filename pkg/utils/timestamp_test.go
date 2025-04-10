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
	"testing"
)

func TestNormalizeFloatTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timestamp float64
		wantSec   int64
		wantNsec  int32
	}{
		{
			name:      "nano seconds",
			timestamp: 1_234_567_890_123_456_789,
			wantSec:   1234567890,
			wantNsec:  123456000,
		},
		{
			name:      "micro seconds",
			timestamp: 1_234_567_890_123_456,
			wantSec:   1234567890,
			wantNsec:  123456000,
		},
		{
			name:      "milli seconds",
			timestamp: 1_234_567_890_123,
			wantSec:   1234567890,
			wantNsec:  123000000,
		},
		{
			name:      "seconds",
			timestamp: 1234567890.123,
			wantSec:   1234567890,
			wantNsec:  123000000,
		},
		{
			name:      "zero seconds",
			timestamp: 0,
			wantSec:   0,
			wantNsec:  0,
		},
		{
			name:      "float seconds",
			timestamp: 1.5,
			wantSec:   1,
			wantNsec:  500000000,
		},
		{
			name:      "high precision seconds",
			timestamp: 1743560172.667562,
			wantSec:   1743560172,
			wantNsec:  667562000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSec, gotNsec := NormalizeFloatTimestamp(tt.timestamp)
			if gotSec != tt.wantSec {
				t.Errorf("NormalizeFloatTimestamp() gotSec = %v, want %v", gotSec, tt.wantSec)
			}
			// keep millisecond precision
			if gotNsec != tt.wantNsec {
				t.Errorf("NormalizeFloatTimestamp() gotNsec = %v, want %v", gotNsec, tt.wantNsec)
			}
		})
	}
}
