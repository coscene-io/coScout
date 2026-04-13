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

import (
	"strings"
	"sync"
)

type recordRegistry struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func newRecordRegistry() *recordRegistry {
	return &recordRegistry{
		active: make(map[string]struct{}),
	}
}

func (r *recordRegistry) TryAcquire(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.active[key]; ok {
		return false
	}

	r.active[key] = struct{}{}
	return true
}

func (r *recordRegistry) Release(key string) {
	if strings.TrimSpace(key) == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, key)
}
