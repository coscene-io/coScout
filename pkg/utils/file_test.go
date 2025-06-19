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
	"os"
	"path/filepath"
	"testing"
)

func TestCheckReadPath(t *testing.T) {
	t.Parallel()

	// Create temporary directory
	tmpDir := t.TempDir()

	// Test empty path
	if CheckReadPath("") {
		t.Error("Empty path should return false")
	}

	// Test non-existent path
	nonExistentPath := filepath.Join(tmpDir, "nonexistent")
	if CheckReadPath(nonExistentPath) {
		t.Error("Non-existent path should return false")
	}

	// Create a readable file
	readableFile := filepath.Join(tmpDir, "readable.txt")
	if err := os.WriteFile(readableFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	if !CheckReadPath(readableFile) {
		t.Error("Readable file should return true")
	}

	// Create a readable directory
	readableDir := filepath.Join(tmpDir, "readable_dir")
	if err := os.Mkdir(readableDir, 0755); err != nil {
		t.Fatal(err)
	}

	if !CheckReadPath(readableDir) {
		t.Error("Readable directory should return true")
	}

	// Test with symlink
	targetFile := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0600); err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Skip("Symlink creation not supported")
	}

	// Symlink to readable file should be readable
	if !CheckReadPath(symlinkPath) {
		t.Error("Symlink to readable file should return true")
	}

	// Test broken symlink
	brokenSymlink := filepath.Join(tmpDir, "broken_symlink")
	nonExistentTarget := filepath.Join(tmpDir, "nonexistent_target")
	if err := os.Symlink(nonExistentTarget, brokenSymlink); err != nil {
		t.Skip("Symlink creation not supported")
	}

	// Broken symlink should return false
	if CheckReadPath(brokenSymlink) {
		t.Error("Broken symlink should return false")
	}
}

func TestGetFileSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a test file with known content
	content := "Hello, World! This is a test file with some content."
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Test regular file
	size, err := GetFileSize(testFile)
	if err != nil {
		t.Fatalf("GetFileSize failed: %v", err)
	}

	expectedSize := int64(len(content))
	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}

	// Test with symlink
	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(testFile, symlinkPath); err != nil {
		t.Skip("Symlink creation not supported")
	}

	// Test GetFileSize (should follow symlink)
	symlinkSize, err := GetFileSize(symlinkPath)
	if err != nil {
		t.Fatalf("GetFileSize for symlink failed: %v", err)
	}

	if symlinkSize != expectedSize {
		t.Errorf("GetFileSize should return target file size %d, got %d", expectedSize, symlinkSize)
	}

	// Test GetFileSizeNoFollow (should return symlink size)
	linkSize, err := GetFileSizeNoFollow(symlinkPath)
	if err != nil {
		t.Fatalf("GetFileSizeNoFollow for symlink failed: %v", err)
	}

	// Symlink size should be different (and typically smaller) than target file size
	if linkSize == expectedSize {
		t.Errorf("GetFileSizeNoFollow should return symlink size, not target file size")
	}

	t.Logf("Target file size: %d", expectedSize)
	t.Logf("GetFileSize (follows symlink): %d", symlinkSize)
	t.Logf("GetFileSizeNoFollow (symlink itself): %d", linkSize)
}

func BenchmarkCheckReadPath_Stat(b *testing.B) {
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		b.Fatal(err)
	}

	// Create a symlink
	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(testFile, symlinkPath); err != nil {
		b.Skip("Symlink creation not supported")
	}

	b.ResetTimer()

	// Benchmark with symlink to show performance difference
	for range b.N {
		CheckReadPath(symlinkPath)
	}
}

func BenchmarkCheckReadPath_RegularFile(b *testing.B) {
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	// Benchmark with regular file
	for range b.N {
		CheckReadPath(testFile)
	}
}
