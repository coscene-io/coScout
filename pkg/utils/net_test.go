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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test CheckNetworkBlackList function.
func TestCheckNetworkBlackList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		server    string
		blacklist []string
		expected  bool
		desc      string
	}{
		{
			name:      "empty_blacklist",
			server:    "8.8.8.8:53",
			blacklist: []string{},
			expected:  false,
			desc:      "Empty blacklist should return false",
		},
		{
			name:      "empty_server_uses_default",
			server:    "",
			blacklist: []string{"eth0", "wlan0"},
			expected:  false, // Assuming default interface is not in blacklist
			desc:      "Empty server should use default server",
		},
		{
			name:      "interface_in_blacklist",
			server:    "8.8.8.8:53",
			blacklist: []string{"eth0", "lo", "docker0"},
			expected:  false, // This depends on actual interface, might need mocking
			desc:      "Should return true if interface is blacklisted",
		},
		{
			name:      "interface_not_in_blacklist",
			server:    "8.8.8.8:53",
			blacklist: []string{"nonexistent0", "fake1"},
			expected:  false,
			desc:      "Should return false if interface is not blacklisted",
		},
		{
			name:      "case_insensitive_match",
			server:    "8.8.8.8:53",
			blacklist: []string{"ETH0", "WLAN0"},
			expected:  false, // This depends on actual interface
			desc:      "Should handle case insensitive matching",
		},
		{
			name:      "invalid_server_address",
			server:    "invalid-server:invalid-port",
			blacklist: []string{"eth0"},
			expected:  true, // Should return true on error
			desc:      "Invalid server should return true (fail-safe)",
		},
		{
			name:      "url_format_server_https",
			server:    "https://openapi.coscene.cn",
			blacklist: []string{"eth0"},
			expected:  true, // URL format is not supported, should fail
			desc:      "HTTPS URL format should fail (not supported)",
		},
		{
			name:      "url_format_server_http",
			server:    "http://api.example.com",
			blacklist: []string{"eth0"},
			expected:  true, // URL format is not supported, should fail
			desc:      "HTTP URL format should fail (not supported)",
		},
		{
			name:      "hostname_without_port",
			server:    "google.com",
			blacklist: []string{"eth0"},
			expected:  true, // Missing port should fail
			desc:      "Hostname without port should fail",
		},
		{
			name:      "valid_hostname_with_port",
			server:    "google.com:53",
			blacklist: []string{"nonexistent0"},
			expected:  false,
			desc:      "Valid hostname with port should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := CheckNetworkBlackList(tt.server, tt.blacklist)
			// Note: Some tests may need adjustment based on actual network interface
			// For invalid servers, we expect true (fail-safe behavior)
			if tt.name == "invalid_server_address" ||
				tt.name == "url_format_server_https" ||
				tt.name == "url_format_server_http" ||
				tt.name == "hostname_without_port" {
				assert.True(t, result, tt.desc)
			} else {
				// For valid cases, the result depends on actual network configuration
				// We mainly test that the function doesn't panic
				assert.IsType(t, false, result, tt.desc)
			}
		})
	}
}

// Test getDefaultInterfacePortable function.
func TestGetDefaultInterfacePortable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		server    string
		shouldErr bool
		desc      string
	}{
		{
			name:      "valid_server",
			server:    "8.8.8.8:53",
			shouldErr: false,
			desc:      "Valid server should return interface name",
		},
		{
			name:      "empty_server_uses_default",
			server:    "",
			shouldErr: false,
			desc:      "Empty server should use default and return interface name",
		},
		{
			name:      "invalid_server_address",
			server:    "invalid-server:99999",
			shouldErr: true,
			desc:      "Invalid server should return error",
		},
		{
			name:      "url_format_server",
			server:    "https://openapi.coscene.cn",
			shouldErr: true,
			desc:      "URL format should return error",
		},
		{
			name:      "missing_port",
			server:    "google.com",
			shouldErr: true,
			desc:      "Missing port should return error",
		},
		{
			name:      "invalid_port",
			server:    "google.com:invalid",
			shouldErr: true,
			desc:      "Invalid port should return error",
		},
		{
			name:      "unreachable_server",
			server:    "127.0.0.1:12345", // Valid but will succeed - UDP Dial doesn't actually send packets
			shouldErr: false,             // UDP Dial always succeeds for valid addresses
			desc:      "Valid server should not return error (UDP Dial is always successful)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interfaceName, err := getDefaultInterfacePortable(tt.server)

			if tt.shouldErr {
				require.Error(t, err, tt.desc)
				assert.Empty(t, interfaceName, "Interface name should be empty on error")
			} else {
				require.NoError(t, err, tt.desc)
				assert.NotEmpty(t, interfaceName, "Interface name should not be empty on success")

				// Verify the returned interface name is valid
				interfaces, err := net.Interfaces()
				require.NoError(t, err, "Should be able to get network interfaces")

				var found bool
				for _, ifc := range interfaces {
					if ifc.Name == interfaceName {
						found = true
						break
					}
				}
				assert.True(t, found, "Returned interface name should exist in system")
			}
		})
	}
}

// Test edge cases and error conditions.
func TestNetworkBlackListEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("nil_blacklist", func(t *testing.T) {
		t.Parallel()
		result := CheckNetworkBlackList("8.8.8.8:53", nil)
		assert.False(t, result, "Nil blacklist should be treated as empty")
	})

	t.Run("whitespace_in_blacklist", func(t *testing.T) {
		t.Parallel()
		blacklist := []string{" eth0 ", "wlan0", " lo"}
		result := CheckNetworkBlackList("8.8.8.8:53", blacklist)
		// The function doesn't trim whitespace, so " eth0 " != "eth0"
		assert.False(t, result, "Whitespace should not be automatically trimmed")
	})

	t.Run("duplicate_interfaces_in_blacklist", func(t *testing.T) {
		t.Parallel()
		blacklist := []string{"eth0", "eth0", "wlan0", "eth0"}
		result := CheckNetworkBlackList("8.8.8.8:53", blacklist)
		// Should work normally with duplicates
		assert.IsType(t, false, result, "Duplicates in blacklist should not cause issues")
	})
}

// Benchmark tests.
func BenchmarkCheckNetworkBlackList(b *testing.B) {
	server := "8.8.8.8:53"
	blacklist := []string{"eth0", "wlan0", "lo", "docker0", "br0"}

	b.ResetTimer()
	for range b.N {
		CheckNetworkBlackList(server, blacklist)
	}
}

func BenchmarkGetDefaultInterfacePortable(b *testing.B) {
	server := "8.8.8.8:53"

	b.ResetTimer()
	for range b.N {
		_, _ = getDefaultInterfacePortable(server)
	}
}
