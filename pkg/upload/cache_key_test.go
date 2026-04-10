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

import "testing"

func TestScopedMultipartCacheKeysDifferByObjectScope(t *testing.T) {
	absPath := "/tmp/bag.tar.gz"

	keyA := GetScopedUploadIdKey("bucket-a", "record-a/files/bag.tar.gz", absPath)
	keyB := GetScopedUploadIdKey("bucket-a", "record-b/files/bag.tar.gz", absPath)
	keyC := GetScopedUploadIdKey("bucket-b", "record-a/files/bag.tar.gz", absPath)

	if keyA == keyB {
		t.Fatalf("expected object key to affect scoped upload cache key")
	}
	if keyA == keyC {
		t.Fatalf("expected bucket to affect scoped upload cache key")
	}
}

func TestLegacyMultipartCacheKeyRemainsPathBased(t *testing.T) {
	absPath := "/tmp/bag.tar.gz"

	legacyA := GetUploadIdKey(absPath)
	legacyB := GetUploadIdKey(absPath)
	scoped := GetScopedUploadIdKey("bucket-a", "record-a/files/bag.tar.gz", absPath)

	if legacyA != legacyB {
		t.Fatalf("expected legacy upload cache key to remain stable for the same path")
	}
	if legacyA == scoped {
		t.Fatalf("expected scoped upload cache key to differ from legacy path-only key")
	}
}
