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

package collector

import "testing"

func TestRecordRegistryTryAcquireAndRelease(t *testing.T) {
	registry := newRecordRegistry()
	key := "/tmp/record/.cos/state.json"

	if !registry.TryAcquire(key) {
		t.Fatalf("expected first acquire to succeed")
	}

	if registry.TryAcquire(key) {
		t.Fatalf("expected second acquire to fail while record is active")
	}

	registry.Release(key)

	if !registry.TryAcquire(key) {
		t.Fatalf("expected acquire to succeed after release")
	}
}
