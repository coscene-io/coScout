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

