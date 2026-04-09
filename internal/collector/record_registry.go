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

