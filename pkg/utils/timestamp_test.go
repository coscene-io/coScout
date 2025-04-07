package utils

import (
	"testing"
)

func TestNormalizeFloatTimestamp(t *testing.T) {
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
