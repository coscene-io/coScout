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
	"fmt"
	"sync"
	"testing"
	"time"
)

const testRecordCachePath = "/tmp/record/.cos/state.json"

func TestKeyedMutexRegistrySerializesSameKey(t *testing.T) {
	t.Parallel()

	registry := newKeyedMutexRegistry()
	unlock := registry.Lock(testRecordCachePath)

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

func TestKeyedMutexRegistrySerializesConcurrentHolders(t *testing.T) {
	t.Parallel()

	const contenders = 8

	registry := newKeyedMutexRegistry()
	key := testRecordCachePath
	releaseHolder := registry.Lock(key)
	start := make(chan struct{})
	entered := make(chan struct{}, contenders)
	leave := make(chan struct{})
	var waiters sync.WaitGroup
	waiters.Add(contenders)

	for i := range contenders {
		go func(index int) {
			defer waiters.Done()
			<-start

			key := testRecordCachePath
			if index%2 == 0 {
				key = "/tmp/record/.cos/../.cos/state.json"
			}
			release := registry.Lock(key)
			defer release()

			entered <- struct{}{}
			<-leave
		}(i)
	}

	close(start)
	waitForKeyedMutexReferences(t, registry, key, contenders+1)
	select {
	case <-entered:
		t.Fatal("a contender acquired the lock while the initial holder still owned it")
	default:
	}
	releaseHolder()

	for i := range contenders {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("contender %d did not acquire the lock", i)
		}

		select {
		case <-entered:
			t.Fatal("multiple goroutines held the same keyed lock concurrently")
		case <-time.After(20 * time.Millisecond):
		}
		leave <- struct{}{}
	}
	waiters.Wait()
}

func TestKeyedMutexRegistryRetainsEntryForHolderAndWaiter(t *testing.T) {
	t.Parallel()

	registry := newKeyedMutexRegistry()
	key := testRecordCachePath
	releaseHolder := registry.Lock(key)

	waiterStarted := make(chan struct{})
	waiterAcquired := make(chan struct{})
	releaseWaiter := make(chan struct{})
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		close(waiterStarted)
		release := registry.Lock(key)
		defer release()
		close(waiterAcquired)
		<-releaseWaiter
	}()

	<-waiterStarted
	waitForKeyedMutexReferences(t, registry, key, 2)
	select {
	case <-waiterAcquired:
		t.Fatal("waiter acquired the lock while the holder still owned it")
	default:
	}

	releaseHolder()
	select {
	case <-waiterAcquired:
	case <-time.After(time.Second):
		t.Fatal("waiter did not acquire the released lock")
	}

	if got := keyedMutexRegistrySize(registry); got != 1 {
		t.Fatalf("registry should retain the entry while the waiter holds it, got %d entries", got)
	}
	if got := keyedMutexRegistryReferences(registry, key); got != 1 {
		t.Fatalf("registry should count the waiter as the only reference after acquisition, got %d", got)
	}

	thirdAcquired := make(chan struct{})
	releaseThird := make(chan struct{})
	thirdDone := make(chan struct{})
	go func() {
		defer close(thirdDone)
		release := registry.Lock(key)
		close(thirdAcquired)
		<-releaseThird
		release()
	}()

	waitForKeyedMutexReferences(t, registry, key, 2)
	select {
	case <-thirdAcquired:
		t.Fatal("a new caller acquired a replacement lock while the waiter still held the key")
	default:
	}

	close(releaseWaiter)
	<-waiterDone
	select {
	case <-thirdAcquired:
	case <-time.After(time.Second):
		t.Fatal("new caller did not acquire the key after the waiter released it")
	}
	if got := keyedMutexRegistryReferences(registry, key); got != 1 {
		t.Fatalf("registry should count the new caller as the only reference, got %d", got)
	}

	close(releaseThird)
	<-thirdDone
	if got := keyedMutexRegistrySize(registry); got != 0 {
		t.Fatalf("registry should remove the entry after the last release, got %d entries", got)
	}
}

func TestKeyedMutexRegistryReclaimsUnusedKeys(t *testing.T) {
	t.Parallel()

	const keyCount = 1_000

	registry := newKeyedMutexRegistry()
	var waiters sync.WaitGroup
	waiters.Add(keyCount)
	for i := range keyCount {
		go func(index int) {
			defer waiters.Done()
			release := registry.Lock(fmt.Sprintf("/tmp/record-%d/.cos/state.json", index))
			release()
		}(i)
	}
	waiters.Wait()

	if got := keyedMutexRegistrySize(registry); got != 0 {
		t.Fatalf("registry retained %d unused keyed locks", got)
	}
}

func TestKeyedMutexRegistryReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	registry := newKeyedMutexRegistry()
	release := registry.Lock(testRecordCachePath)
	release()
	release()

	if got := keyedMutexRegistrySize(registry); got != 0 {
		t.Fatalf("registry retained %d entries after repeated release", got)
	}

	release = registry.Lock(testRecordCachePath)
	release()
	if got := keyedMutexRegistrySize(registry); got != 0 {
		t.Fatalf("registry retained %d entries after key reuse", got)
	}
}

func keyedMutexRegistrySize(registry *keyedMutexRegistry) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.locks)
}

func waitForKeyedMutexReferences(t *testing.T, registry *keyedMutexRegistry, key string, expected int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		if got := keyedMutexRegistryReferences(registry, key); got == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("keyed lock did not reach %d references", expected)
		}
		time.Sleep(time.Millisecond)
	}
}

func keyedMutexRegistryReferences(registry *keyedMutexRegistry, key string) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	lock := registry.locks[key]
	if lock == nil {
		return 0
	}
	return lock.references
}
