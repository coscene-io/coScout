package collector

import (
	"strings"
	"sync"
)

type uploadLockRegistry struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func newUploadLockRegistry() *uploadLockRegistry {
	return &uploadLockRegistry{
		active: make(map[string]struct{}),
	}
}

func (r *uploadLockRegistry) TryAcquire(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.active[key]; exists {
		return false
	}

	r.active[key] = struct{}{}
	return true
}

func (r *uploadLockRegistry) Release(key string) {
	if strings.TrimSpace(key) == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, key)
}

var inFlightUploadLocks = newUploadLockRegistry()

