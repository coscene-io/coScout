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
	// Found 9 Go files:
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
