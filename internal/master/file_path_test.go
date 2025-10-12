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

package master

import (
	"strings"
	"testing"
)

func TestSlaveFilePathHandling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		slaveID       string
		remotePath    string
		expectedPath  string
		expectedValid bool
	}{
		{
			name:          "Valid slave file path",
			slaveID:       "a1b2c3d4e5f60789",
			remotePath:    "/home/user/data.log",
			expectedPath:  "slave://a1b2c3d4e5f60789/home/user/data.log",
			expectedValid: true,
		},
		{
			name:          "Remote path without leading slash",
			slaveID:       "a1b2c3d4e5f60789",
			remotePath:    "home/user/data.log",
			expectedPath:  "slave://a1b2c3d4e5f60789/home/user/data.log",
			expectedValid: true,
		},
		{
			name:          "Root file",
			slaveID:       "a1b2c3d4e5f60789",
			remotePath:    "/data.log",
			expectedPath:  "slave://a1b2c3d4e5f60789/data.log",
			expectedValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test formatting
			formatted := FormatSlaveFilePath(tt.slaveID, tt.remotePath)
			if formatted != tt.expectedPath {
				t.Errorf("FormatSlaveFilePath() = %v, want %v", formatted, tt.expectedPath)
			}

			// Test if it's recognized as slave file
			if !IsSlaveFile(formatted) {
				t.Errorf("IsSlaveFile() returned false for %v", formatted)
			}

			// Test parsing
			parsedSlaveID, parsedRemotePath, err := ParseSlaveFilePath(formatted)
			if err != nil {
				t.Errorf("ParseSlaveFilePath() error = %v", err)
				return
			}

			if parsedSlaveID != tt.slaveID {
				t.Errorf("ParseSlaveFilePath() slaveID = %v, want %v", parsedSlaveID, tt.slaveID)
			}

			expectedRemotePath := tt.remotePath
			if !strings.HasPrefix(expectedRemotePath, "/") {
				expectedRemotePath = "/" + expectedRemotePath
			}

			if parsedRemotePath != expectedRemotePath {
				t.Errorf("ParseSlaveFilePath() remotePath = %v, want %v", parsedRemotePath, expectedRemotePath)
			}
		})
	}
}

func TestSlaveIDValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		slaveID string
		valid   bool
	}{
		{
			name:    "Valid 16-char alphanumeric ID",
			slaveID: "a1b2c3d4e5f60789",
			valid:   true,
		},
		{
			name:    "Valid 16-char uppercase ID",
			slaveID: "A1B2C3D4E5F60789",
			valid:   true,
		},
		{
			name:    "Valid 16-char mixed case ID",
			slaveID: "A1b2C3d4E5f60789",
			valid:   true,
		},
		{
			name:    "Invalid - too short",
			slaveID: "a1b2c3d4e5f6078",
			valid:   false,
		},
		{
			name:    "Invalid - too long",
			slaveID: "a1b2c3d4e5f607890",
			valid:   false,
		},
		{
			name:    "Invalid - contains special characters",
			slaveID: "a1b2-3d4e5f60789",
			valid:   false,
		},
		{
			name:    "Invalid - contains underscore",
			slaveID: "a1b2_3d4e5f60789",
			valid:   false,
		},
		{
			name:    "Invalid - empty",
			slaveID: "",
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidSlaveID(tt.slaveID)
			if result != tt.valid {
				t.Errorf("IsValidSlaveID(%v) = %v, want %v", tt.slaveID, result, tt.valid)
			}
		})
	}
}

func TestInvalidSlaveFilePaths(t *testing.T) {
	t.Parallel()
	invalidPaths := []string{
		"regular/local/path.log",
		"file://local/path.log",
		"slave://",
		"slave://invalidid",
		"slave://a1b2c3d4e5f60789", // missing path
		"slave:///absolute/path",   // missing slave ID
	}

	for _, path := range invalidPaths {
		t.Run("Invalid path: "+path, func(t *testing.T) {
			t.Parallel()
			if IsSlaveFile(path) && path != "slave://a1b2c3d4e5f60789" {
				// Only slave:// prefix should be detected, but parsing should fail
				_, _, err := ParseSlaveFilePath(path)
				if err == nil {
					t.Errorf("ParseSlaveFilePath() should have failed for %v", path)
				}
			} else if !IsSlaveFile(path) {
				// Non-slave paths should not be detected as slave files
				if path == "slave://a1b2c3d4e5f60789" {
					t.Errorf("IsSlaveFile() should have returned true for %v", path)
				}
			}
		})
	}
}

func TestSlaveFilePathExamples(t *testing.T) {
	t.Parallel()
	// Test realistic examples
	examples := []struct {
		description string
		slaveID     string
		remotePath  string
	}{
		{
			description: "Log file in /var/log",
			slaveID:     "f47ac10b58cc4372",
			remotePath:  "/var/log/system.log",
		},
		{
			description: "Data file in home directory",
			slaveID:     "6ba7b8119dad11e1",
			remotePath:  "/home/robot/sensor_data.csv",
		},
		{
			description: "Config file in /etc",
			slaveID:     "b0018a1e9ce44c2f",
			remotePath:  "/etc/ros/setup.bash",
		},
		{
			description: "Temporary file",
			slaveID:     "c36c9f8f7b8b4e5a",
			remotePath:  "/tmp/debug_output.txt",
		},
	}

	for _, example := range examples {
		t.Run(example.description, func(t *testing.T) {
			t.Parallel()
			// Format the path
			slavePath := FormatSlaveFilePath(example.slaveID, example.remotePath)

			// Verify it's a valid slave path
			if !IsSlaveFile(slavePath) {
				t.Errorf("Formatted path %v should be recognized as slave file", slavePath)
			}

			// Parse it back
			parsedID, parsedPath, err := ParseSlaveFilePath(slavePath)
			if err != nil {
				t.Errorf("Failed to parse slave path %v: %v", slavePath, err)
			}

			if parsedID != example.slaveID {
				t.Errorf("Parsed slave ID %v != original %v", parsedID, example.slaveID)
			}

			if parsedPath != example.remotePath {
				t.Errorf("Parsed remote path %v != original %v", parsedPath, example.remotePath)
			}

			// Verify slave ID is valid
			if !IsValidSlaveID(example.slaveID) {
				t.Errorf("Slave ID %v should be valid", example.slaveID)
			}

			t.Logf("✓ %s: %s", example.description, slavePath)
		})
	}
}
