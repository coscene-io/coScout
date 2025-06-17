// Copyright 2025 coScene
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

package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var errOther = errors.New("other error")

// Example: basic usage.
func ExampleSymWalk_basic() {
	// Use default configuration to traverse current directory
	err := SymWalk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error accessing %s: %v\n", path, err)
			return nil
		}

		if info.IsDir() {
			fmt.Printf("Directory: %s\n", path)
		} else {
			fmt.Printf("File: %s (size: %d)\n", path, info.Size())
		}
		return nil
	}, nil)

	if err != nil {
		fmt.Printf("Traversal failed: %v\n", err)
	}
	// Output:
	// Directory: .
	// File: conf.go (size: 1133)
	// File: file.go (size: 2934)
	// File: file_test.go (size: 4828)
	// File: symwalk.go (size: 6415)
	// File: symwalk_test.go (size: 11797)
	// File: timestamp.go (size: 2238)
	// File: timestamp_test.go (size: 2037)
	// File: utils.go (size: 755)
	// File: utils_test.go (size: 1665)
}

// Example: custom configuration.
func ExampleSymWalk_custom() {
	// Custom configuration
	options := &SymWalkOptions{
		FollowSymlinks:       false, // don't follow symlinks
		SkipPermissionErrors: true,  // skip permission errors
	}

	err := SymWalk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip errors in example
		}

		// only process .txt files
		if !info.IsDir() && filepath.Ext(path) == ".txt" {
			fmt.Printf("Found text file: %s\n", path)
		}
		return nil
	}, options)

	if err != nil {
		fmt.Printf("Traversal failed: %v\n", err)
	}
	// Output:
	//
}

// Test default configuration.
func TestDefaultSymWalkOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultSymWalkOptions()

	if !opts.FollowSymlinks {
		t.Error("Expected FollowSymlinks to be true")
	}

	if !opts.SkipPermissionErrors {
		t.Error("Expected SkipPermissionErrors to be true")
	}
}

// Test permission error detection.
func TestIsPermissionError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"permission error", os.ErrPermission, true},
		{"other error", errOther, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isPermissionError(tt.err)
			if result != tt.expected {
				t.Errorf("isPermissionError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// Test symlink detection.
func TestIsSymlink(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a regular file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink
	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(testFile, symlinkPath); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}

	// Test regular file
	fileInfo, err := os.Lstat(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if IsSymlink(fileInfo) {
		t.Error("Regular file should not be detected as symlink")
	}

	// Test symlink
	symlinkInfo, err := os.Lstat(symlinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSymlink(symlinkInfo) {
		t.Error("Symlink should be detected as symlink")
	}
}

// Test symlink cycle detection.
func TestSymWalk_CycleDetection(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create directories
	dirA := filepath.Join(tmpDir, "a")
	dirB := filepath.Join(tmpDir, "a", "b")
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink that creates a cycle: a/b/link_to_a -> a
	symlinkPath := filepath.Join(dirB, "link_to_a")
	if err := os.Symlink(dirA, symlinkPath); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}

	visitedPaths := make(map[string]bool)

	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Don't ignore errors in cycle detection test
		}

		if visitedPaths[path] {
			t.Errorf("Path visited twice: %s", path)
		}
		visitedPaths[path] = true
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Should have visited all paths without infinite loop
	expectedPaths := []string{tmpDir, dirA, dirB, symlinkPath}
	for _, expectedPath := range expectedPaths {
		if !visitedPaths[expectedPath] {
			t.Errorf("Expected path not visited: %s", expectedPath)
		}
	}
}

// Test that symlinks are not followed when FollowSymlinks is false.
func TestSymWalk_NoFollowSymlinks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a file and directory
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0600); err != nil {
		t.Fatal(err)
	}

	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatal(err)
	}

	nestedFile := filepath.Join(testDir, "nested.txt")
	if err := os.WriteFile(nestedFile, []byte("nested content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create symlinks
	symlinkToFile := filepath.Join(tmpDir, "link_to_file")
	symlinkToDir := filepath.Join(tmpDir, "link_to_dir")

	if err := os.Symlink(testFile, symlinkToFile); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}
	if err := os.Symlink(testDir, symlinkToDir); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}

	options := &SymWalkOptions{
		FollowSymlinks:       false,
		SkipPermissionErrors: true,
	}

	var visitedPaths []string
	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Don't ignore errors in this test
		}
		visitedPaths = append(visitedPaths, path)
		return nil
	}, options)

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Should visit symlinks but not their targets
	expectedVisits := map[string]bool{
		tmpDir:        true,
		testFile:      true,
		testDir:       true,
		nestedFile:    true,
		symlinkToFile: true,
		symlinkToDir:  true,
	}

	for _, path := range visitedPaths {
		if !expectedVisits[path] {
			t.Errorf("Unexpected path visited: %s", path)
		}
		delete(expectedVisits, path)
	}

	for path := range expectedVisits {
		t.Errorf("Expected path not visited: %s", path)
	}
}

// Test deterministic output (lexical order).
func TestSymWalk_DeterministicOrder(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create files in non-alphabetical order to test sorting
	filenames := []string{"zebra.txt", "alpha.txt", "beta.txt"}
	for _, name := range filenames {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	var visitedFiles []string
	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Don't ignore errors in this test
		}

		if !info.IsDir() {
			visitedFiles = append(visitedFiles, filepath.Base(path))
		}
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Files should be visited in alphabetical order
	expectedOrder := []string{"alpha.txt", "beta.txt", "zebra.txt"}
	if len(visitedFiles) != len(expectedOrder) {
		t.Fatalf("Expected %d files, got %d", len(expectedOrder), len(visitedFiles))
	}

	for i, expected := range expectedOrder {
		if visitedFiles[i] != expected {
			t.Errorf("Expected file at position %d to be %s, got %s", i, expected, visitedFiles[i])
		}
	}
}

// Test SkipDir functionality.
func TestSymWalk_SkipDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create directory structure
	skipDir := filepath.Join(tmpDir, "skip_me")
	normalDir := filepath.Join(tmpDir, "normal")

	if err := os.MkdirAll(skipDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(normalDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create files in both directories
	if err := os.WriteFile(filepath.Join(skipDir, "skipped.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normalDir, "normal.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	var visitedPaths []string
	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Don't ignore errors in this test
		}

		visitedPaths = append(visitedPaths, path)

		// Skip the skip_me directory
		if info.IsDir() && filepath.Base(path) == "skip_me" {
			return filepath.SkipDir
		}
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Check that skipped.txt was not visited
	for _, path := range visitedPaths {
		if filepath.Base(path) == "skipped.txt" {
			t.Error("Files in skipped directory should not be visited")
		}
	}

	// Check that normal.txt was visited
	found := false
	for _, path := range visitedPaths {
		if filepath.Base(path) == "normal.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Files in normal directory should be visited")
	}
}

// Benchmark test.
func BenchmarkSymWalk(b *testing.B) {
	// Create temporary directory structure for testing
	tmpDir := b.TempDir()

	// Create some test files
	for range 10 {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", len(os.Args)))
		if err := os.WriteFile(filename, []byte("test content"), 0600); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for range b.N {
		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			return nil // do nothing, just test traversal performance
		}, nil)

		if err != nil {
			b.Fatal(err)
		}
	}
}

// Real-world usage example: find specific files.
func ExampleSymWalk_findFiles() {
	var foundFiles []string

	// Find all .go files
	err := SymWalk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip errors in example
		}

		if !info.IsDir() && filepath.Ext(path) == ".go" {
			foundFiles = append(foundFiles, path)
		}
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}

	// Sort for consistent output
	sort.Strings(foundFiles)

	fmt.Printf("Found %d Go files\n", len(foundFiles))
	for _, file := range foundFiles {
		fmt.Printf("- %s\n", file)
	}
	// Output:
	// Found 9 Go files
	// - conf.go
	// - file.go
	// - file_test.go
	// - symwalk.go
	// - symwalk_test.go
	// - timestamp.go
	// - timestamp_test.go
	// - utils.go
	// - utils_test.go
}

// Real-world usage example: calculate directory size.
func ExampleSymWalk_calculateSize() {
	var totalSize int64
	var fileCount int

	err := SymWalk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip errors in example
		}

		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		fmt.Printf("Size calculation failed: %v\n", err)
		return
	}

	fmt.Printf("Total %d files, total size %d bytes\n", fileCount, totalSize)
	// Output:
	// Total 9 files, total size 33802 bytes
}
