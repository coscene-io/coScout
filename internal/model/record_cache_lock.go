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
	"path/filepath"
	"strings"
	"sync"
)

type keyedMutexRegistry struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKeyedMutexRegistry() *keyedMutexRegistry {
	return &keyedMutexRegistry{
		locks: make(map[string]*sync.Mutex),
	}
}

func (r *keyedMutexRegistry) Lock(key string) func() {
	key = filepath.Clean(strings.TrimSpace(key))
	if key == "" || key == "." {
		return func() {}
	}

	r.mu.Lock()
	lock, ok := r.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		r.locks[key] = lock
	}
	r.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}

var recordCacheFileLocks = newKeyedMutexRegistry() //nolint:gochecknoglobals // package-level singleton registry for record cache file locking
