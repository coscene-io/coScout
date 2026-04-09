package collector

import "testing"

func TestUploadLockRegistryTryAcquireAndRelease(t *testing.T) {
	registry := newUploadLockRegistry()
	key := "record/files/file.tar.gz|/tmp/file.tar.gz"

	if !registry.TryAcquire(key) {
		t.Fatalf("expected first acquire to succeed")
	}

	if registry.TryAcquire(key) {
		t.Fatalf("expected second acquire to fail while key is held")
	}

	registry.Release(key)

	if !registry.TryAcquire(key) {
		t.Fatalf("expected acquire to succeed after release")
	}
}

