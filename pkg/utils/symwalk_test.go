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
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var (
	errOther         = errors.New("other error")
	errWalkFilePaths = errors.New("callback failed")
)

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
	// Found 11 Go files
	// - conf.go
	// - file.go
	// - file_test.go
	// - net.go
	// - net_test.go
	// - symwalk.go
	// - symwalk_test.go
	// - timestamp.go
	// - timestamp_test.go
	// - utils.go
	// - utils_test.go
}

// Test GetAllFilePaths basic functionality.
func TestGetAllFilePaths_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files and directories
	testFiles := []string{"file1.txt", "file2.go", "file3.md"}
	testDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create files in root directory
	for _, filename := range testFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, filename), []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Create files in subdirectory
	subFiles := []string{"subfile1.txt", "subfile2.go"}
	for _, filename := range subFiles {
		if err := os.WriteFile(filepath.Join(testDir, filename), []byte("subcontent"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Test with default options
	paths, err := GetAllFilePaths(tmpDir, nil)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should have 5 files total (3 in root + 2 in subdirectory)
	expectedCount := 5
	if len(paths) != expectedCount {
		t.Errorf("Expected %d files, got %d", expectedCount, len(paths))
	}

	// Check that all paths are absolute
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Errorf("Path should be absolute: %s", path)
		}
	}

	// Check that no directories are included
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Cannot stat path %s: %v", path, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("Directory should not be included in result: %s", path)
		}
	}

	// Check that files are in lexical order (should be deterministic)
	for i := 1; i < len(paths); i++ {
		if paths[i-1] >= paths[i] {
			t.Errorf("Paths are not in lexical order: %s >= %s", paths[i-1], paths[i])
		}
	}
}

// Test GetAllFilePaths with empty directory.
func TestGetAllFilePaths_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	paths, err := GetAllFilePaths(tmpDir, nil)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("Expected empty result for empty directory, got %d files", len(paths))
	}
}

// Test GetAllFilePaths with symbolic links.
func TestGetAllFilePaths_Symlinks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a regular file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create a directory with files
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

	// Test with following symlinks (default behavior)
	options := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
	}

	paths, err := GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should include: test.txt, nested.txt, link_to_file, and nested.txt through link_to_dir
	// Note: the exact count may vary depending on symlink handling, but should be >= 3
	if len(paths) < 3 {
		t.Errorf("Expected at least 3 files when following symlinks, got %d", len(paths))
	}

	// Test without following symlinks
	options.FollowSymlinks = false
	paths, err = GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should include: test.txt, nested.txt, link_to_file (symlink treated as file), link_to_dir (symlink treated as file)
	// So we expect 4 files total
	expectedCount := 4
	if len(paths) != expectedCount {
		t.Errorf("Expected %d files when not following symlinks, got %d", expectedCount, len(paths))
	}
}

// Test GetAllFilePaths with non-existent directory.
func TestGetAllFilePaths_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	paths, err := GetAllFilePaths("/non/existent/path", nil)
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
	if paths != nil {
		t.Error("Expected nil paths for non-existent directory")
	}
}

// Test GetAllFilePaths with custom options.
func TestGetAllFilePaths_CustomOptions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Test with custom options
	options := &SymWalkOptions{
		FollowSymlinks:       false,
		SkipPermissionErrors: false,
	}

	paths, err := GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	if len(paths) != 2 {
		t.Errorf("Expected 2 files, got %d", len(paths))
	}

	file1 := filepath.Join(tmpDir, "file1.txt")
	filePaths, err := GetAllFilePaths(file1, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(filePaths) != 1 {
		t.Errorf("Expected absolute path for file1.txt, got %s", filePaths)
	}
}

// Test GetAllFilePaths with nested directories.
func TestGetAllFilePaths_NestedDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create nested directory structure
	// tmpDir/
	//   ├── file1.txt
	//   ├── dir1/
	//   │   ├── file2.txt
	//   │   └── dir2/
	//   │       └── file3.txt
	//   └── dir3/
	//       └── file4.txt

	dirs := []string{
		filepath.Join(tmpDir, "dir1"),
		filepath.Join(tmpDir, "dir1", "dir2"),
		filepath.Join(tmpDir, "dir3"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		filepath.Join(tmpDir, "file1.txt"):                 "content1",
		filepath.Join(tmpDir, "dir1", "file2.txt"):         "content2",
		filepath.Join(tmpDir, "dir1", "dir2", "file3.txt"): "content3",
		filepath.Join(tmpDir, "dir3", "file4.txt"):         "content4",
	}

	for filePath, content := range files {
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := GetAllFilePaths(tmpDir, nil)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	if len(paths) != 4 {
		t.Errorf("Expected 4 files, got %d", len(paths))
	}

	// Verify all expected files are present
	foundFiles := make(map[string]bool)
	for _, path := range paths {
		foundFiles[path] = true
	}

	for expectedPath := range files {
		absExpected, err := filepath.Abs(expectedPath)
		if err != nil {
			t.Fatal(err)
		}
		if !foundFiles[absExpected] {
			t.Errorf("Expected file not found: %s", absExpected)
		}
	}
}

// Benchmark GetAllFilePaths.
func BenchmarkGetAllFilePaths(b *testing.B) {
	tmpDir := b.TempDir()

	// Create test files
	for i := range 100 {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(filename, []byte("test content"), 0600); err != nil {
			b.Fatal(err)
		}
	}

	// Create some subdirectories with files
	for i := range 10 {
		subdir := filepath.Join(tmpDir, fmt.Sprintf("subdir%d", i))
		if err := os.Mkdir(subdir, 0755); err != nil {
			b.Fatal(err)
		}
		for j := range 10 {
			filename := filepath.Join(subdir, fmt.Sprintf("subfile%d.txt", j))
			if err := os.WriteFile(filename, []byte("test content"), 0600); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ResetTimer()

	for range b.N {
		_, err := GetAllFilePaths(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Example usage of GetAllFilePaths with custom options.
func ExampleGetAllFilePaths_customOptions() {
	// Configure options to not follow symlinks and skip permission errors
	options := &SymWalkOptions{
		FollowSymlinks:       false,
		SkipPermissionErrors: true,
	}

	paths, err := GetAllFilePaths(".", options)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Filter for specific file types
	var goFiles []string
	for _, path := range paths {
		if filepath.Ext(path) == ".go" {
			goFiles = append(goFiles, path)
		}
	}

	fmt.Printf("Found %d Go files:\n", len(goFiles))
	for _, path := range goFiles {
		fmt.Printf("- %s\n", filepath.Base(path))
	}
	// Output:
	// Found 11 Go files:
	// - conf.go
	// - file.go
	// - file_test.go
	// - net.go
	// - net_test.go
	// - symwalk.go
	// - symwalk_test.go
	// - timestamp.go
	// - timestamp_test.go
	// - utils.go
	// - utils_test.go
}

// Test GetAllFilePaths with MaxFiles limit.
func TestGetAllFilePaths_MaxFilesLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create more files than the limit
	maxFiles := 5
	totalFiles := 10

	for i := range totalFiles {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(filename, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Test with MaxFiles limit
	options := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		MaxFiles:             maxFiles,
	}

	paths, err := GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should have collected exactly maxFiles before hitting the limit
	if len(paths) != maxFiles {
		t.Errorf("Expected exactly %d files, got %d", maxFiles, len(paths))
	}
}

func TestWalkFilePathsProcessesFilesBeforeLaterTraversalError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	firstPath := filepath.Join(root, "a-file.txt")
	if err := os.WriteFile(firstPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "z-broken-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var visited []string
	err := WalkFilePaths(root, &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: false,
		SkipEmptyFiles:       true,
		MaxFiles:             99999,
	}, func(filePath string) error {
		visited = append(visited, filePath)
		return nil
	})

	if err == nil {
		t.Fatal("WalkFilePaths() error = nil, want broken symlink traversal error")
	}
	if len(visited) != 1 || visited[0] != firstPath {
		t.Fatalf("visited = %v, want first file processed before later traversal error", visited)
	}
}

func TestWalkFilePathsMatchesLegacyCollector(t *testing.T) {
	t.Parallel()

	t.Run("directory order, absolute paths, empty files, and limits", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		nested := filepath.Join(root, "b-dir")
		if err := os.Mkdir(nested, 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		for path, content := range map[string]string{
			filepath.Join(root, "a.txt"):          "a",
			filepath.Join(root, "c-empty.txt"):    "",
			filepath.Join(nested, "a-empty.txt"):  "",
			filepath.Join(nested, "b-nested.txt"): "nested",
		} {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}

		cases := []struct {
			name    string
			options *SymWalkOptions
		}{
			{name: "nil defaults", options: nil},
			{
				name: "include empty and unlimited",
				options: &SymWalkOptions{
					FollowSymlinks:       true,
					SkipPermissionErrors: true,
					SkipEmptyFiles:       false,
					MaxFiles:             0,
				},
			},
			{
				name: "limit one",
				options: &SymWalkOptions{
					FollowSymlinks:       true,
					SkipPermissionErrors: true,
					SkipEmptyFiles:       false,
					MaxFiles:             1,
				},
			},
			{
				name: "limit at exact file count",
				options: &SymWalkOptions{
					FollowSymlinks:       true,
					SkipPermissionErrors: true,
					SkipEmptyFiles:       false,
					MaxFiles:             4,
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				assertWalkFilePathsMatchesLegacy(t, root, tc.options)
			})
		}
	})

	t.Run("file roots", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		nonEmpty := filepath.Join(root, "non-empty.txt")
		empty := filepath.Join(root, "empty.txt")
		if err := os.WriteFile(nonEmpty, []byte("data"), 0o600); err != nil {
			t.Fatalf("write non-empty root: %v", err)
		}
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatalf("write empty root: %v", err)
		}

		assertWalkFilePathsMatchesLegacy(t, nonEmpty, nil)
		assertWalkFilePathsMatchesLegacy(t, empty, nil)
		assertWalkFilePathsMatchesLegacy(t, empty, &SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       false,
			MaxFiles:             0,
		})
	})

	t.Run("relative root becomes absolute", func(t *testing.T) {
		t.Parallel()

		filePath := filepath.Join(t.TempDir(), "relative.txt")
		if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
			t.Fatalf("write relative-root file: %v", err)
		}
		workingDir, err := os.Getwd()
		if err != nil {
			t.Fatalf("get working directory: %v", err)
		}
		relativePath, err := filepath.Rel(workingDir, filePath)
		if err != nil {
			t.Fatalf("make relative path: %v", err)
		}
		assertWalkFilePathsMatchesLegacy(t, relativePath, nil)
	})
}

func TestWalkFilePathsMatchesLegacySymlinkVisitedBehavior(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetFile := filepath.Join(root, "z-target.txt")
	if err := os.WriteFile(targetFile, []byte("target"), 0o600); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	targetDir := filepath.Join(root, "z-target-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	targetDirFile := filepath.Join(targetDir, "file.txt")
	if err := os.WriteFile(targetDirFile, []byte("nested"), 0o600); err != nil {
		t.Fatalf("write target dir file: %v", err)
	}

	for link, target := range map[string]string{
		filepath.Join(root, "a-file-link"): targetFile,
		filepath.Join(root, "b-file-link"): targetFile,
		filepath.Join(root, "c-dir-link"):  targetDir,
		filepath.Join(root, "d-dir-link"):  targetDir,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	followOptions := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: false,
		SkipEmptyFiles:       false,
		MaxFiles:             0,
	}
	assertWalkFilePathsMatchesLegacy(t, root, followOptions)
	followedPaths, err := legacyGetAllFilePathsForTest(root, followOptions)
	if err != nil {
		t.Fatalf("legacy followed collector error = %v", err)
	}
	wantFollowed := []string{
		filepath.Join(root, "a-file-link"),
		filepath.Join(root, "c-dir-link", "file.txt"),
		targetDirFile,
		targetFile,
	}
	if !reflect.DeepEqual(followedPaths, wantFollowed) {
		t.Fatalf("followed paths = %v, want visited-target order %v", followedPaths, wantFollowed)
	}

	noFollowOptions := &SymWalkOptions{
		FollowSymlinks:       false,
		SkipPermissionErrors: false,
		SkipEmptyFiles:       false,
		MaxFiles:             0,
	}
	assertWalkFilePathsMatchesLegacy(t, root, noFollowOptions)
	notFollowedPaths, err := legacyGetAllFilePathsForTest(root, noFollowOptions)
	if err != nil {
		t.Fatalf("legacy non-followed collector error = %v", err)
	}
	wantNotFollowed := []string{
		filepath.Join(root, "a-file-link"),
		filepath.Join(root, "b-file-link"),
		filepath.Join(root, "c-dir-link"),
		filepath.Join(root, "d-dir-link"),
		targetDirFile,
		targetFile,
	}
	if !reflect.DeepEqual(notFollowedPaths, wantNotFollowed) {
		t.Fatalf("non-followed paths = %v, want %v", notFollowedPaths, wantNotFollowed)
	}
}

func TestWalkFilePathsMatchesLegacyBrokenAndCyclicSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("broken symlink", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		broken := filepath.Join(root, "broken")
		if err := os.Symlink(filepath.Join(root, "missing"), broken); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		assertWalkFilePathsMatchesLegacy(t, root, &SymWalkOptions{
			FollowSymlinks:       false,
			SkipPermissionErrors: false,
			SkipEmptyFiles:       false,
			MaxFiles:             0,
		})

		legacyPaths, legacyErr := legacyGetAllFilePathsForTest(root, &SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: false,
			SkipEmptyFiles:       false,
			MaxFiles:             0,
		})
		currentPaths, currentErr := GetAllFilePaths(root, &SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: false,
			SkipEmptyFiles:       false,
			MaxFiles:             0,
		})
		if legacyErr == nil || currentErr == nil {
			t.Fatalf("legacy error = %v, current error = %v; both must reject a followed broken link", legacyErr, currentErr)
		}
		if legacyPaths != nil || currentPaths != nil {
			t.Fatalf("legacy paths = %v, current paths = %v; both must be atomic on error", legacyPaths, currentPaths)
		}
	})

	t.Run("directory cycle", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		dir := filepath.Join(root, "dir")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mkdir cycle dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o600); err != nil {
			t.Fatalf("write cycle file: %v", err)
		}
		if err := os.Symlink(root, filepath.Join(dir, "back-to-root")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		assertWalkFilePathsMatchesLegacy(t, root, &SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: false,
			SkipEmptyFiles:       false,
			MaxFiles:             0,
		})
	})
}

func TestWalkFilePathsPermissionErrorCompatibility(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}

	root := t.TempDir()
	firstPath := filepath.Join(root, "a-file.txt")
	if err := os.WriteFile(firstPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	deniedDir := filepath.Join(root, "z-denied")
	if err := os.Mkdir(deniedDir, 0o700); err != nil {
		t.Fatalf("mkdir denied dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deniedDir, "hidden.txt"), []byte("hidden"), 0o600); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}
	if err := os.Chmod(deniedDir, 0); err != nil {
		t.Fatalf("chmod denied dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(deniedDir, 0o700)
	})
	if _, err := os.ReadDir(deniedDir); err == nil {
		t.Skip("test process can read mode-000 directories")
	}

	assertWalkFilePathsMatchesLegacy(t, root, &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		SkipEmptyFiles:       false,
		MaxFiles:             0,
	})

	strictOptions := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: false,
		SkipEmptyFiles:       false,
		MaxFiles:             0,
	}
	legacyPaths, legacyErr := legacyGetAllFilePathsForTest(root, strictOptions)
	currentPaths, currentErr := GetAllFilePaths(root, strictOptions)
	if !errors.Is(legacyErr, os.ErrPermission) || !errors.Is(currentErr, os.ErrPermission) {
		t.Fatalf("legacy error = %v, current error = %v; both must propagate permission failure", legacyErr, currentErr)
	}
	if legacyPaths != nil || currentPaths != nil {
		t.Fatalf("legacy paths = %v, current paths = %v; collectors must be atomic on permission failure", legacyPaths, currentPaths)
	}

	var streamed []string
	streamErr := WalkFilePaths(root, strictOptions, func(filePath string) error {
		streamed = append(streamed, filePath)
		return nil
	})
	if !errors.Is(streamErr, os.ErrPermission) {
		t.Fatalf("WalkFilePaths() error = %v, want permission failure", streamErr)
	}
	if !reflect.DeepEqual(streamed, []string{firstPath}) {
		t.Fatalf("streamed = %v, want callback before later permission failure", streamed)
	}
}

func TestWalkFilePathsCallbackControlErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("data"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	options := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: false,
		SkipEmptyFiles:       false,
		MaxFiles:             0,
	}

	t.Run("ordinary error propagates", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := WalkFilePaths(root, options, func(string) error {
			calls++
			return errWalkFilePaths
		})
		if !errors.Is(err, errWalkFilePaths) {
			t.Fatalf("WalkFilePaths() error = %v, want %v", err, errWalkFilePaths)
		}
		if calls != 1 {
			t.Fatalf("callback calls = %d, want 1", calls)
		}
	})

	t.Run("SkipDir stops successfully", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := WalkFilePaths(root, options, func(string) error {
			calls++
			return filepath.SkipDir
		})
		if err != nil {
			t.Fatalf("WalkFilePaths() error = %v, want nil", err)
		}
		if calls != 1 {
			t.Fatalf("callback calls = %d, want 1", calls)
		}
	})

	t.Run("SkipAll propagates for caller classification", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := WalkFilePaths(root, options, func(string) error {
			calls++
			return filepath.SkipAll
		})
		if !errors.Is(err, filepath.SkipAll) {
			t.Fatalf("WalkFilePaths() error = %v, want filepath.SkipAll", err)
		}
		if calls != 1 {
			t.Fatalf("callback calls = %d, want 1", calls)
		}
	})
}

func assertWalkFilePathsMatchesLegacy(t *testing.T, root string, options *SymWalkOptions) {
	t.Helper()

	legacyPaths, legacyErr := legacyGetAllFilePathsForTest(root, options)
	if legacyErr != nil {
		t.Fatalf("legacy collector error = %v", legacyErr)
	}

	var streamedPaths []string
	streamErr := WalkFilePaths(root, options, func(filePath string) error {
		streamedPaths = append(streamedPaths, filePath)
		return nil
	})
	if streamErr != nil {
		t.Fatalf("WalkFilePaths() error = %v", streamErr)
	}
	if !reflect.DeepEqual(streamedPaths, legacyPaths) {
		t.Fatalf("WalkFilePaths() = %v, legacy collector = %v", streamedPaths, legacyPaths)
	}

	currentPaths, currentErr := GetAllFilePaths(root, options)
	if currentErr != nil {
		t.Fatalf("GetAllFilePaths() error = %v", currentErr)
	}
	if !reflect.DeepEqual(currentPaths, legacyPaths) {
		t.Fatalf("GetAllFilePaths() = %v, legacy collector = %v", currentPaths, legacyPaths)
	}
}

// legacyGetAllFilePathsForTest independently reconstructs the implementation
// from main before GetAllFilePaths delegated to WalkFilePaths.
func legacyGetAllFilePathsForTest(root string, options *SymWalkOptions) ([]string, error) {
	if options == nil {
		options = DefaultSymWalkOptions()
	}

	var filePaths []string
	err := SymWalk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if options.MaxFiles > 0 && len(filePaths) >= options.MaxFiles {
			return filepath.SkipDir
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		filePaths = append(filePaths, absPath)
		return nil
	}, options)
	if err != nil {
		return nil, err
	}
	return filePaths, nil
}

// Test GetAllFilePaths with MaxFiles set to 0 (no limit).
func TestGetAllFilePaths_NoMaxFilesLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create some test files
	totalFiles := 15
	for i := range totalFiles {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(filename, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Test with MaxFiles set to 0 (no limit)
	options := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		MaxFiles:             0, // No limit
	}

	paths, err := GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should have collected all files
	if len(paths) != totalFiles {
		t.Errorf("Expected %d files, got %d", totalFiles, len(paths))
	}
}

// Test GetAllFilePaths with default options (should have MaxFiles limit).
func TestGetAllFilePaths_DefaultMaxFiles(t *testing.T) {
	t.Parallel()

	// Test that default options include MaxFiles limit
	defaultOpts := DefaultSymWalkOptions()
	if defaultOpts.MaxFiles <= 0 {
		t.Error("Default options should have a positive MaxFiles limit")
	}

	expectedMaxFiles := 10000
	if defaultOpts.MaxFiles != expectedMaxFiles {
		t.Errorf("Expected default MaxFiles to be %d, got %d", expectedMaxFiles, defaultOpts.MaxFiles)
	}
}

// Test GetAllFilePaths MaxFiles limit with nested directories.
func TestGetAllFilePaths_MaxFilesWithNestedDirs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	maxFiles := 3

	// Create nested structure with files
	// tmpDir/
	//   ├── file1.txt
	//   ├── dir1/
	//   │   ├── file2.txt
	//   │   └── file3.txt
	//   └── dir2/
	//       ├── file4.txt  // This should trigger the limit
	//       └── file5.txt

	// Root level file
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create dir1 with files
	dir1 := filepath.Join(tmpDir, "dir1")
	if err := os.Mkdir(dir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "file2.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "file3.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create dir2 with files (these should trigger the limit)
	dir2 := filepath.Join(tmpDir, "dir2")
	if err := os.Mkdir(dir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "file4.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "file5.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	options := &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		MaxFiles:             maxFiles,
	}

	paths, err := GetAllFilePaths(tmpDir, options)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should have collected exactly maxFiles before hitting the limit
	if len(paths) != maxFiles {
		t.Errorf("Expected exactly %d files, got %d", maxFiles, len(paths))
	}
}

// Test GetAllFilePaths only returns non-empty files.
func TestGetAllFilePaths_OnlyNonEmptyFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a mix of empty and non-empty files
	files := map[string]string{
		"empty1.txt":    "",          // Empty file
		"empty2.txt":    "",          // Empty file
		"nonempty1.txt": "content 1", // Non-empty file
		"nonempty2.txt": "content 2", // Non-empty file
		"empty3.txt":    "",          // Empty file
		"nonempty3.txt": "content 3", // Non-empty file
	}

	for filename, content := range files {
		filePath := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Get all file paths
	paths, err := GetAllFilePaths(tmpDir, nil)
	if err != nil {
		t.Fatalf("GetAllFilePaths failed: %v", err)
	}

	// Should only have 3 non-empty files
	expectedCount := 3
	if len(paths) != expectedCount {
		t.Errorf("Expected %d non-empty files, got %d", expectedCount, len(paths))
	}

	// Verify all returned files are non-empty
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Cannot stat file %s: %v", path, err)
			continue
		}
		if info.Size() <= 0 {
			t.Errorf("Empty file should not be included: %s (size: %d)", path, info.Size())
		}
	}

	// Verify that only non-empty files are returned
	foundFiles := make(map[string]bool)
	for _, path := range paths {
		foundFiles[filepath.Base(path)] = true
	}

	// Check that non-empty files are included
	expectedNonEmptyFiles := []string{"nonempty1.txt", "nonempty2.txt", "nonempty3.txt"}
	for _, filename := range expectedNonEmptyFiles {
		if !foundFiles[filename] {
			t.Errorf("Non-empty file should be included: %s", filename)
		}
	}

	// Check that empty files are not included
	unexpectedEmptyFiles := []string{"empty1.txt", "empty2.txt", "empty3.txt"}
	for _, filename := range unexpectedEmptyFiles {
		if foundFiles[filename] {
			t.Errorf("Empty file should not be included: %s", filename)
		}
	}
}

// Test SymWalk with SkipEmptyFiles option.
func TestSymWalk_SkipEmptyFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a mix of empty and non-empty files
	files := map[string]string{
		"empty1.txt":    "",          // Empty file
		"nonempty1.txt": "content 1", // Non-empty file
		"empty2.log":    "",          // Empty file
		"nonempty2.log": "content 2", // Non-empty file
	}

	for filename, content := range files {
		filePath := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Test with SkipEmptyFiles = true (default)
	t.Run("SkipEmptyFiles enabled", func(t *testing.T) {
		t.Parallel()
		var visitedFiles []string
		options := &SymWalkOptions{
			SkipEmptyFiles: true,
		}

		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				visitedFiles = append(visitedFiles, filepath.Base(path))
			}
			return nil
		}, options)

		if err != nil {
			t.Fatalf("SymWalk failed: %v", err)
		}

		// Should only visit non-empty files
		expectedCount := 2
		if len(visitedFiles) != expectedCount {
			t.Errorf("Expected %d non-empty files, got %d: %v", expectedCount, len(visitedFiles), visitedFiles)
		}

		// Check that only non-empty files were visited
		for _, filename := range visitedFiles {
			if filename == "empty1.txt" || filename == "empty2.log" {
				t.Errorf("Empty file should not be visited: %s", filename)
			}
		}
	})

	// Test with SkipEmptyFiles = false
	t.Run("SkipEmptyFiles disabled", func(t *testing.T) {
		t.Parallel()
		var visitedFiles []string
		options := &SymWalkOptions{
			SkipEmptyFiles: false,
		}

		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				visitedFiles = append(visitedFiles, filepath.Base(path))
			}
			return nil
		}, options)

		if err != nil {
			t.Fatalf("SymWalk failed: %v", err)
		}

		// Should visit all files
		expectedCount := 4
		if len(visitedFiles) != expectedCount {
			t.Errorf("Expected %d files, got %d: %v", expectedCount, len(visitedFiles), visitedFiles)
		}
	})
}

// Test that default options skip empty files.
func TestSymWalk_DefaultSkipsEmptyFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create an empty file and a non-empty file
	emptyFile := filepath.Join(tmpDir, "empty.txt")
	nonEmptyFile := filepath.Join(tmpDir, "nonempty.txt")

	if err := os.WriteFile(emptyFile, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonEmptyFile, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	var visitedFiles []string

	// Use nil options to test default behavior
	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			visitedFiles = append(visitedFiles, filepath.Base(path))
		}
		return nil
	}, nil)

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Default should skip empty files
	if len(visitedFiles) != 1 {
		t.Errorf("Expected 1 file with default options, got %d: %v", len(visitedFiles), visitedFiles)
	}

	if len(visitedFiles) > 0 && visitedFiles[0] != "nonempty.txt" {
		t.Errorf("Expected nonempty.txt, got %s", visitedFiles[0])
	}
}

// Benchmark SymWalk with optimized implementation.
func BenchmarkSymWalkOptimized(b *testing.B) {
	// Create a test directory structure
	tmpDir := b.TempDir()

	// Create nested directories with files
	for i := range 10 {
		subdir := filepath.Join(tmpDir, fmt.Sprintf("dir%d", i))
		if err := os.Mkdir(subdir, 0755); err != nil {
			b.Fatal(err)
		}

		// Create files in each subdirectory
		for j := range 100 {
			filename := filepath.Join(subdir, fmt.Sprintf("file%d.txt", j))
			if err := os.WriteFile(filename, []byte("test content"), 0600); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ResetTimer()
	for range b.N {
		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			return nil
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark GetAllFilePaths with optimized implementation.
func BenchmarkGetAllFilePathsOptimized(b *testing.B) {
	// Create a test directory structure
	tmpDir := b.TempDir()

	// Create files
	for i := range 1000 {
		filename := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(filename, []byte("test content"), 0600); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for range b.N {
		_, err := GetAllFilePaths(tmpDir, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Test self-referencing symlinks handling.
func TestSymWalk_SelfReferencingSymlink(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a self-referencing symlink: a -> a
	selfLinkPath := filepath.Join(tmpDir, "self_link")
	if err := os.Symlink("self_link", selfLinkPath); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}

	// Create another variant: link points to itself with absolute path
	absLinkPath := filepath.Join(tmpDir, "abs_self_link")
	if err := os.Symlink(absLinkPath, absLinkPath); err != nil {
		// If creation fails, try to create it anyway
		t.Logf("Note: Could not create absolute self-referencing symlink: %v", err)
	}

	// Create a regular file for comparison
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Test with default options (follow symlinks)
	t.Run("FollowSymlinks enabled", func(t *testing.T) {
		t.Parallel()
		var visitedPaths []string
		var errors []error

		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				errors = append(errors, err)
				return nil // Continue walking
			}
			visitedPaths = append(visitedPaths, path)
			return nil
		}, DefaultSymWalkOptions())

		if err != nil {
			t.Fatalf("SymWalk failed: %v", err)
		}

		// Should not have any "too many links" errors
		for _, e := range errors {
			if e != nil && strings.Contains(e.Error(), "too many links") {
				t.Errorf("Got 'too many links' error: %v", e)
			}
		}

		// Should have visited the directory and regular file
		foundRegular := false
		foundDir := false
		for _, path := range visitedPaths {
			if path == tmpDir {
				foundDir = true
			}
			if path == regularFile {
				foundRegular = true
			}
		}

		if !foundDir {
			t.Error("Should have visited the directory")
		}
		if !foundRegular {
			t.Error("Should have visited the regular file")
		}
	})

	// Test without following symlinks
	t.Run("FollowSymlinks disabled", func(t *testing.T) {
		t.Parallel()
		var visitedPaths []string

		options := &SymWalkOptions{
			FollowSymlinks:       false,
			SkipPermissionErrors: true,
		}

		err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil //nolint:nilerr // Continue walking despite errors in this test
			}
			visitedPaths = append(visitedPaths, path)
			return nil
		}, options)

		if err != nil {
			t.Fatalf("SymWalk failed: %v", err)
		}

		// Should visit the symlinks as regular files
		foundSelfLink := false
		for _, path := range visitedPaths {
			if path == selfLinkPath {
				foundSelfLink = true
			}
		}

		if !foundSelfLink {
			t.Error("Should have visited the self-referencing symlink when not following links")
		}
	})
}

// Test complex circular symlink chains.
func TestSymWalk_CircularSymlinkChain(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a circular chain: a -> b -> c -> a
	linkA := filepath.Join(tmpDir, "link_a")
	linkB := filepath.Join(tmpDir, "link_b")
	linkC := filepath.Join(tmpDir, "link_c")

	if err := os.Symlink("link_b", linkA); err != nil {
		t.Skip("Symlink creation not supported on this system")
	}
	if err := os.Symlink("link_c", linkB); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("link_a", linkC); err != nil {
		t.Fatal(err)
	}

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	var visitedPaths []string
	var errors []error

	err := SymWalk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errors = append(errors, err)
			return nil // Continue walking
		}
		visitedPaths = append(visitedPaths, path)
		return nil
	}, DefaultSymWalkOptions())

	if err != nil {
		t.Fatalf("SymWalk failed: %v", err)
	}

	// Should not have any "too many links" errors
	for _, e := range errors {
		if e != nil && strings.Contains(e.Error(), "too many links") {
			t.Errorf("Got 'too many links' error: %v", e)
		}
	}

	// Should have visited the directory and regular file
	foundRegular := false
	for _, path := range visitedPaths {
		if path == regularFile {
			foundRegular = true
		}
	}

	if !foundRegular {
		t.Error("Should have visited the regular file despite circular symlinks")
	}
}
