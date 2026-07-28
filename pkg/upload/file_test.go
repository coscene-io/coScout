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

package upload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeUploadFilesInvalidWindowDoesNotTouchFilesystem(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	files, noPermissionPaths, err := ComputeUploadFiles(
		"invalid-window",
		[]string{missingPath},
		[]string{missingPath},
		nil,
		true,
		200,
		100,
	)

	if !errors.Is(err, ErrInvalidTimeWindow) {
		t.Fatalf("error = %v, want ErrInvalidTimeWindow", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none because validation must precede filesystem access", noPermissionPaths)
	}
}

func TestComputeUploadFilesFutureStartDoesNotTouchFilesystem(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	now := time.Now()
	files, noPermissionPaths, err := ComputeUploadFiles(
		"future-window",
		[]string{missingPath},
		[]string{missingPath},
		nil,
		true,
		now.Add(10*time.Minute).Unix(),
		now.Add(20*time.Minute).Unix(),
	)

	if !errors.Is(err, ErrTimeWindowNotReady) {
		t.Fatalf("error = %v, want ErrTimeWindowNotReady", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none because validation must precede filesystem access", noPermissionPaths)
	}
}

func TestComputeUploadFilesFutureEndStillSelectsCurrentFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := writeUploadTestFile(t, root, "current.log")
	now := time.Now()
	if err := os.Chtimes(filePath, now, now); err != nil {
		t.Fatalf("set file times: %v", err)
	}

	files, noPermissionPaths, err := ComputeUploadFiles(
		"future-end",
		[]string{root},
		nil,
		[]string{"**/*.log"},
		true,
		now.Add(-time.Minute).Unix(),
		now.Add(time.Hour).Unix(),
	)

	if err != nil {
		t.Fatalf("ComputeUploadFiles() error = %v", err)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none", noPermissionPaths)
	}
	if _, ok := files[filePath]; !ok {
		t.Fatalf("files = %v, want current file %s selected when end is in the future", files, filePath)
	}
}

func TestComputeUploadFilesCharacterizesRecursiveWhitelistSymlinkAndAdditionalFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nestedDir := filepath.Join(root, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	matchedPath := writeUploadTestFile(t, nestedDir, "matched.log")
	_ = writeUploadTestFile(t, nestedDir, "ignored.txt")

	externalDir := t.TempDir()
	externalPath := writeUploadTestFile(t, externalDir, "linked.log")
	symlinkPath := filepath.Join(root, "linked.log")
	if err := os.Symlink(externalPath, symlinkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	externalPath, err := filepath.EvalSymlinks(externalPath)
	if err != nil {
		t.Fatalf("resolve external file: %v", err)
	}

	additionalDir := t.TempDir()
	additionalPath := writeUploadTestFile(t, additionalDir, "always.bin")

	now := time.Now()
	files, noPermissionPaths, err := ComputeUploadFiles(
		"characterization",
		[]string{root},
		[]string{additionalDir},
		[]string{"**/*.log"},
		true,
		now.Add(-time.Hour).Unix(),
		now.Add(time.Hour).Unix(),
	)

	if err != nil {
		t.Fatalf("ComputeUploadFiles() error = %v", err)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none", noPermissionPaths)
	}
	for _, expected := range []string{matchedPath, externalPath, additionalPath} {
		if _, ok := files[expected]; !ok {
			t.Errorf("files = %v, missing %s", files, expected)
		}
	}
	if got, want := len(files), 3; got != want {
		t.Fatalf("len(files) = %d, want %d: %v", got, want, files)
	}
}

func TestComputeUploadFilesCharacterizesNonRecursiveScan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directPath := writeUploadTestFile(t, root, "direct.log")
	nestedDir := filepath.Join(root, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	nestedPath := writeUploadTestFile(t, nestedDir, "nested.log")

	now := time.Now()
	files, noPermissionPaths, err := ComputeUploadFiles(
		"non-recursive",
		[]string{root},
		nil,
		[]string{"**/*.log"},
		false,
		now.Add(-time.Hour).Unix(),
		now.Add(time.Hour).Unix(),
	)

	if err != nil {
		t.Fatalf("ComputeUploadFiles() error = %v", err)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none", noPermissionPaths)
	}
	if _, ok := files[directPath]; !ok {
		t.Fatalf("files = %v, want direct file %s", files, directPath)
	}
	if _, ok := files[nestedPath]; ok {
		t.Fatalf("files = %v, nested file %s must not be selected", files, nestedPath)
	}
}

func TestComputeUploadFilesDiscardsDirectoryResultsAfterTraversalError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_ = writeUploadTestFile(t, root, "a-file.log")
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "z-broken-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	now := time.Now()
	files, noPermissionPaths, err := ComputeUploadFiles(
		"traversal-error",
		[]string{root},
		nil,
		nil,
		true,
		now.Add(-time.Hour).Unix(),
		now.Add(time.Hour).Unix(),
	)

	if err != nil {
		t.Fatalf("ComputeUploadFiles() error = %v, want directory traversal errors handled per existing semantics", err)
	}
	if len(noPermissionPaths) != 0 {
		t.Fatalf("no-permission paths = %v, want none", noPermissionPaths)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want the failed directory's partial results discarded", files)
	}
}

func TestComputeUploadFilesPreservesUnreadablePathReporting(t *testing.T) {
	t.Parallel()

	missingScanPath := filepath.Join(t.TempDir(), "missing-scan")
	missingAdditionalPath := filepath.Join(t.TempDir(), "missing-additional")
	now := time.Now()
	files, noPermissionPaths, err := ComputeUploadFiles(
		"unreadable-paths",
		[]string{missingScanPath},
		[]string{missingAdditionalPath},
		nil,
		true,
		now.Add(-time.Hour).Unix(),
		now.Unix(),
	)

	if err != nil {
		t.Fatalf("ComputeUploadFiles() error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
	if len(noPermissionPaths) != 2 ||
		noPermissionPaths[0] != missingScanPath ||
		noPermissionPaths[1] != missingAdditionalPath {
		t.Fatalf(
			"no-permission paths = %v, want [%s %s]",
			noPermissionPaths,
			missingScanPath,
			missingAdditionalPath,
		)
	}
}

func writeUploadTestFile(t *testing.T, dir, name string) string {
	t.Helper()

	filePath := filepath.Join(dir, name)
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
	return filePath
}
