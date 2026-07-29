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
	locks map[string]*keyedMutex
}

type keyedMutex struct {
	mu         sync.Mutex
	references int
}

func newKeyedMutexRegistry() *keyedMutexRegistry {
	return &keyedMutexRegistry{
		locks: make(map[string]*keyedMutex),
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
		lock = &keyedMutex{}
		r.locks[key] = lock
	}
	lock.references++
	r.mu.Unlock()

	lock.mu.Lock()

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			r.release(key, lock)
		})
	}
}

func (r *keyedMutexRegistry) release(key string, lock *keyedMutex) {
	// Keep the registry locked until the keyed mutex is unlocked. When the last
	// reference is removed, a new caller cannot create a replacement entry
	// until the old mutex no longer has a holder.
	r.mu.Lock()
	lock.references--
	if lock.references == 0 && r.locks[key] == lock {
		delete(r.locks, key)
	}
	lock.mu.Unlock()
	r.mu.Unlock()
}

var recordCacheFileLocks = newKeyedMutexRegistry() //nolint:gochecknoglobals // package-level singleton registry for record cache file locking
