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

func TestGetStringOrDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		str1       string
		str2       string
		defaultStr string
		expected   string
	}{
		{
			name:       "both strings are not empty",
			str1:       "str1",
			str2:       "str2",
			defaultStr: "default",
			expected:   "str1",
		},
		{
			name:       "str1 is empty, str2 is not empty",
			str1:       "",
			str2:       "str2",
			defaultStr: "default",
			expected:   "str2",
		},
		{
			name:       "str1 is not empty, str2 is empty",
			str1:       "str1",
			str2:       "",
			defaultStr: "default",
			expected:   "str1",
		},
		{
			name:       "both strings are empty",
			str1:       "",
			str2:       "",
			defaultStr: "default",
			expected:   "default",
		},
	}

	for _, test := range tests {
		result := GetStringOrDefault(test.defaultStr, test.str1, test.str2)
		if result != test.expected {
			t.Errorf("GetStringOrDefault(%q, %q, %q) = %q; expected %q", test.str1, test.str2, test.expected, result, test.expected)
		}
	}
}

func TestIsValidDeviceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		deviceID string
		expected bool
	}{
		// Valid device IDs
		{"valid alphanumeric", "device123", true},
		{"valid with dash", "device-123", true},
		{"valid with underscore", "device_123", true},
		{"valid with dot", "device.123", true},
		{"valid mixed", "device-123_456.789", true},
		{"valid uppercase", "DEVICE123", true},
		{"valid mixed case", "Device123", true},
		{"valid single char", "a", true},
		{"valid numbers only", "123456", true},
		{"valid letters only", "device", true},

		// Valid with new symbols
		{"valid with exclamation", "device!123", true},
		{"valid with at", "device@123", true},
		{"valid with hash", "device#123", true},
		{"valid with percent", "device%123", true},
		{"valid with caret", "device^123", true},
		{"valid with ampersand", "device&123", true},
		{"valid with asterisk", "device*123", true},
		{"valid with parentheses", "device(123)", true},
		{"valid with plus", "device+123", true},
		{"valid with equals", "device=123", true},
		{"valid with brackets", "device[123]", true},
		{"valid with braces", "device{123}", true},
		{"valid with colon", "device:123", true},
		{"valid with semicolon", "device;123", true},
		{"valid with comma", "device,123", true},
		{"valid with question mark", "device?123", true},
		{"valid with tilde", "device~123", true},
		{"valid complex symbols", "device!@#$%^&*()+={}", true},

		// Invalid device IDs
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"with space", "device 123", false},
		{"with slash", "device/123", false},
		{"with backslash", "device\\123", false},
		{"with quote", "device'123", false},
		{"with double quote", "device\"123", false},
		{"with pipe", "device|123", false},
		{"with backtick", "device`123", false},
		{"with dollar", "device$123", true},
		{"with less than", "device<123", false},
		{"with greater than", "device>123", false},
		{"too long", "a123456789012345678901234567890123456789012345678901234567890123456789", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidDeviceID(tt.deviceID)
			if result != tt.expected {
				t.Errorf("IsValidDeviceID(%q) = %v, expected %v", tt.deviceID, result, tt.expected)
			}
		})
	}
}
