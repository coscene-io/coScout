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
