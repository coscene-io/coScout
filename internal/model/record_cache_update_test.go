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
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var errStopRecordCacheUpdate = errors.New("stop update")

func TestUpdateRecordCacheMergesLatestState(t *testing.T) {
	t.Parallel()

	recordCachePath := filepath.Join(t.TempDir(), "record", ".cos", "state.json")
	initial := &RecordCache{
		ProjectName:       "projects/p1",
		Timestamp:         1,
		UploadedFilePaths: []string{"/tmp/a.bag"},
		DiagnosisTask: map[string]interface{}{
			"rule_name": "rules/r1",
		},
	}
	if err := writeRecordCacheFile(recordCachePath, initial); err != nil {
		t.Fatalf("write initial record cache: %v", err)
	}

	if _, err := UpdateRecordCache(recordCachePath, func(rc *RecordCache) error {
		rc.Skipped = true
		return nil
	}); err != nil {
		t.Fatalf("set skipped: %v", err)
	}

	if _, err := UpdateRecordCache(recordCachePath, func(rc *RecordCache) error {
		rc.UploadedFilePaths = append(rc.UploadedFilePaths, "/tmp/b.bag")
		rc.DiagnosisTask["name"] = "tasks/t1"
		return nil
	}); err != nil {
		t.Fatalf("append uploaded file: %v", err)
	}

	updated, err := LoadRecordCache(recordCachePath)
	if err != nil {
		t.Fatalf("load updated record cache: %v", err)
	}
	if !updated.Skipped {
		t.Fatal("expected skipped flag to be preserved")
	}
	if !slices.Contains(updated.UploadedFilePaths, "/tmp/a.bag") || !slices.Contains(updated.UploadedFilePaths, "/tmp/b.bag") {
		t.Fatalf("expected uploaded file paths to be merged, got %v", updated.UploadedFilePaths)
	}
	if got := updated.DiagnosisTask["rule_name"]; got != "rules/r1" {
		t.Fatalf("expected existing diagnosis task field to be preserved, got %v", got)
	}
	if got := updated.DiagnosisTask["name"]; got != "tasks/t1" {
		t.Fatalf("expected diagnosis task name to be updated, got %v", got)
	}
}

func TestUpdateRecordCacheDoesNotWriteOnMutatorError(t *testing.T) {
	t.Parallel()

	recordCachePath := filepath.Join(t.TempDir(), "record", ".cos", "state.json")
	initial := &RecordCache{ProjectName: "projects/p1", Timestamp: 1}
	if err := writeRecordCacheFile(recordCachePath, initial); err != nil {
		t.Fatalf("write initial record cache: %v", err)
	}

	if _, err := UpdateRecordCache(recordCachePath, func(rc *RecordCache) error {
		rc.Uploaded = true
		return errStopRecordCacheUpdate
	}); !errors.Is(err, errStopRecordCacheUpdate) {
		t.Fatalf("expected mutator error, got %v", err)
	}

	updated, err := LoadRecordCache(recordCachePath)
	if err != nil {
		t.Fatalf("load updated record cache: %v", err)
	}
	if updated.Uploaded {
		t.Fatal("record cache should not be written when mutator fails")
	}
}

func TestUpdateRecordCacheRejectsNilMutator(t *testing.T) {
	t.Parallel()

	if _, err := UpdateRecordCache(filepath.Join(t.TempDir(), "record", ".cos", "state.json"), nil); err == nil {
		t.Fatal("expected nil mutator to be rejected")
	}
}

func TestLoadRecordCachePreservesSourcePathForInstanceMethods(t *testing.T) {
	t.Parallel()

	recordCachePath := filepath.Join(t.TempDir(), "legacy-record", ".cos", "state.json")
	initial := &RecordCache{
		ProjectName: "projects/p1",
		Timestamp:   1,
	}
	if err := writeRecordCacheFile(recordCachePath, initial); err != nil {
		t.Fatalf("write initial record cache: %v", err)
	}

	rc, err := LoadRecordCache(recordCachePath)
	if err != nil {
		t.Fatalf("load record cache: %v", err)
	}
	if got := rc.GetRecordCachePath(); got != recordCachePath {
		t.Fatalf("expected loaded cache path %s, got %s", recordCachePath, got)
	}
	if got := rc.GetBaseFolder(); got != filepath.Dir(filepath.Dir(recordCachePath)) {
		t.Fatalf("expected loaded base folder to come from path, got %s", got)
	}

	if _, err := rc.Update(func(latest *RecordCache) error {
		latest.Uploaded = true
		return nil
	}); err != nil {
		t.Fatalf("update loaded record cache: %v", err)
	}

	updated, err := LoadRecordCache(recordCachePath)
	if err != nil {
		t.Fatalf("reload updated record cache: %v", err)
	}
	if !updated.Uploaded {
		t.Fatal("expected update to write back to the original path")
	}
}

func TestRecordCachePathValidation(t *testing.T) {
	t.Parallel()

	if _, err := LoadRecordCache(" "); err == nil {
		t.Fatal("expected empty load path to be rejected")
	}
	if _, err := UpdateRecordCache(" ", func(*RecordCache) error { return nil }); err == nil {
		t.Fatal("expected empty update path to be rejected")
	}
}

func TestReloadDoesNotReplaceInstanceMutex(t *testing.T) {
	t.Parallel()

	recordCachePath := filepath.Join(t.TempDir(), "record", ".cos", "state.json")
	initial := &RecordCache{ProjectName: "projects/p1", Timestamp: 1}
	if err := writeRecordCacheFile(recordCachePath, initial); err != nil {
		t.Fatalf("write initial record cache: %v", err)
	}

	rc, err := LoadRecordCache(recordCachePath)
	if err != nil {
		t.Fatalf("load record cache: %v", err)
	}

	if _, err := UpdateRecordCache(recordCachePath, func(latest *RecordCache) error {
		latest.ProjectName = "projects/p2"
		return nil
	}); err != nil {
		t.Fatalf("update record cache: %v", err)
	}

	if _, err := rc.Reload(); err != nil {
		t.Fatalf("reload record cache: %v", err)
	}
	if rc.ProjectName != "projects/p2" {
		t.Fatalf("expected project name to be reloaded, got %s", rc.ProjectName)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = rc.GetRecordCachePath()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("record cache mutex appears to be unusable after reload")
	}
}
