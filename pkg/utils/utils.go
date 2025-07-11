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
	"regexp"
	"strings"
)

func GetStringOrDefault(defaultStr string, strs ...string) string {
	for _, str := range strs {
		if str != "" {
			return str
		}
	}
	return defaultStr
}

// IsValidDeviceID validates that a device ID contains only alphanumeric characters, numbers and allowed symbols.
// Allowed characters: letters (a-z, A-Z), numbers (0-9), and symbols.
// Device ID must be between 1 and 64 characters long.
func IsValidDeviceID(deviceID string) bool {
	// Check if deviceID is empty
	if strings.TrimSpace(deviceID) == "" {
		return false
	}

	// Check length (1-64)
	if len(deviceID) < 1 || len(deviceID) > 64 {
		return false
	}

	// Define the regex pattern for valid device ID
	// Allow: letters (a-z, A-Z), numbers (0-9), and symbols
	// Symbols: . _ - ! @ # $ % ^ & * ( ) + = [ ] { } : ; , ? ~
	pattern := `^[a-zA-Z0-9._!@#$%^&*()+={}\[\]:;,?~-]+$`
	matched, err := regexp.MatchString(pattern, deviceID)
	if err != nil {
		return false
	}

	return matched
}
