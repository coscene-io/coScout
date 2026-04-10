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

package model

import (
	"testing"
	"time"
)

func TestKeyedMutexRegistrySerializesSameKey(t *testing.T) {
	registry := newKeyedMutexRegistry()
	unlock := registry.Lock("/tmp/record/.cos/state.json")

	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		release := registry.Lock("/tmp/record/.cos/../.cos/state.json")
		defer release()
	}()

	select {
	case <-acquired:
		t.Fatal("lock for the same cleaned key should block until released")
	case <-time.After(20 * time.Millisecond):
	}

	unlock()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lock for the same key was not released")
	}
}
