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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// SymWalkOptions contains simple configuration options for SymWalk.
type SymWalkOptions struct {
	// FollowSymlinks when true, follows symbolic links. When false, treats symlinks as regular files.
	FollowSymlinks bool

	// SkipPermissionErrors when true, skips directories/files that cannot be accessed due to permission errors
	// instead of returning an error.
	SkipPermissionErrors bool

	// MaxFiles limits the maximum number of files to collect in GetAllFilePaths.
	// When set to 0, no limit is applied. Default is 10000.
	// This helps prevent memory issues when traversing large directory structures
	// or when symbolic links point to root directories.
	MaxFiles int

	// SkipEmptyFiles when true, skips files with size 0 to save processing time and memory.
	// This is useful when empty files are not relevant for the use case.
	SkipEmptyFiles bool
}

// DefaultSymWalkOptions returns a default configuration.
func DefaultSymWalkOptions() *SymWalkOptions {
	return &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		MaxFiles:             10000,
		SkipEmptyFiles:       true,
	}
}

// SymWalk walks the file tree rooted at root, calling walkFn for each file or
// directory in the tree, including root. It supports following symbolic links
// safely by preventing infinite loops. The files are walked in lexical order,
// which makes the output deterministic. By default, empty files (size 0) are
// skipped to save processing time and memory.
//
// This implementation uses os.DirEntry to reduce system calls and improve performance.
//
// root: the starting path for traversal
// walkFn: the function called for each file or directory (uses standard filepath.WalkFunc)
// options: configuration options, if nil, DefaultSymWalkOptions() is used
func SymWalk(root string, walkFn filepath.WalkFunc, options *SymWalkOptions) error {
	if options == nil {
		options = DefaultSymWalkOptions()
	}

	// Track visited real paths to prevent cycles
	visited := make(map[string]bool)

	info, err := os.Lstat(root)
	if err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		err = walkFn(root, nil, err)
	} else {
		err = symwalk(root, info, walkFn, visited, options)
	}
	if errors.Is(err, filepath.SkipDir) {
		return nil
	}
	return err
}

// handleSymlink processes symbolic links.
func handleSymlink(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// First, read the symlink target without resolving it
	linkTarget, err := os.Readlink(path)
	if err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		return walkFn(path, info, err)
	}

	// Get absolute path of the symlink itself
	absPath, err := filepath.Abs(path)
	if err != nil {
		return walkFn(path, info, err)
	}

	// Check if symlink points to itself (directly)
	absLinkTarget := linkTarget
	if !filepath.IsAbs(linkTarget) {
		// If target is relative, make it absolute based on symlink's directory
		absLinkTarget = filepath.Join(filepath.Dir(absPath), linkTarget)
	}

	// Clean the paths for comparison
	cleanAbsPath := filepath.Clean(absPath)
	cleanAbsLinkTarget := filepath.Clean(absLinkTarget)

	if cleanAbsPath == cleanAbsLinkTarget {
		log.Warnf("Skipping self-referencing symlink: %s", path)
		return nil // Skip self-referencing symlinks
	}

	// Resolve symlink to real path
	resolved, err := filepath.EvalSymlinks(path)
	//nolint: nestif // This is necessary to handle symlinks correctly
	if err != nil {
		// Check if it's a "too many links" error
		// Different OS may return different error types, so we check both the error type and the error message
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			var errno syscall.Errno
			if errors.As(pathErr.Err, &errno) && errors.Is(errno, syscall.ELOOP) {
				log.Debugf("Skipping symlink with circular reference: %s", path)
				return nil // Skip circular symlinks
			}
		}

		// Also check the error message as a fallback (for systems that don't use PathError)
		errStr := err.Error()
		if strings.Contains(errStr, "too many links") || strings.Contains(errStr, "too many levels") {
			log.Warnf("Skipping symlink with circular reference: %s", path)
			return nil // Skip circular symlinks
		}

		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		return walkFn(path, info, err)
	}

	// Get absolute path of the resolved path to prevent cycles
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return walkFn(path, info, err)
	}

	// Check if we've already visited this real path (prevent cycles)
	if visited[absResolved] {
		return nil // Skip already visited paths
	}

	// Mark as visited
	visited[absResolved] = true

	// Get file info of the resolved path
	if info, err = os.Lstat(resolved); err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		return walkFn(path, info, err)
	}

	// Recursively walk the symlinked path, but pass the original path to walkFn
	if err := symwalk(path, info, walkFn, visited, options); err != nil && !errors.Is(err, filepath.SkipDir) {
		return err
	}
	return nil
}

// symwalk is the optimized recursive walker using os.DirEntry.
func symwalk(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Handle symbolic links
	if IsSymlink(info) && options.FollowSymlinks {
		return handleSymlink(path, info, walkFn, visited, options)
	}

	// Skip empty files if configured to do so
	if options.SkipEmptyFiles && !info.IsDir() && info.Size() == 0 {
		return nil
	}

	// Call walkFn for current file/directory
	if err := walkFn(path, info, nil); err != nil {
		return err
	}

	// If it's not a directory, we're done
	if !info.IsDir() {
		return nil
	}

	return walkDirectory(path, walkFn, visited, options)
}

// walkDirectory uses os.ReadDir for better performance.
func walkDirectory(path string, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Read directory entries (more efficient than readDirNames + Lstat)
	entries, err := os.ReadDir(path)
	if err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		// Call walkFn with the error for the directory
		info, _ := os.Lstat(path)
		return walkFn(path, info, err)
	}

	// Sort entries for deterministic order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// Process each entry
	for _, entry := range entries {
		filename := filepath.Join(path, entry.Name())
		if err := processEntry(entry, filename, walkFn, visited, options); err != nil {
			return err
		}
	}

	return nil
}

// processEntry processes a directory entry and returns whether to continue processing.
func processEntry(entry os.DirEntry, filename string, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Get FileInfo from DirEntry (avoids extra stat call for non-symlinks)
	info, err := entry.Info()
	if err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil // Skip this entry
		}
		if err := walkFn(filename, nil, err); err != nil && !errors.Is(err, filepath.SkipDir) {
			return err
		}
		return nil // Skip this entry
	}

	// For symlinks, we need to use Lstat to get the symlink info
	//nolint: nestif // This is necessary to handle symlinks correctly
	if entry.Type()&os.ModeSymlink != 0 {
		info, err = os.Lstat(filename)
		if err != nil {
			if options.SkipPermissionErrors && isPermissionError(err) {
				return nil // Skip this entry
			}
			if err := walkFn(filename, info, err); err != nil && !errors.Is(err, filepath.SkipDir) {
				return err
			}
			return nil // Skip this entry
		}
	}

	// Recursively process the entry
	err = symwalk(filename, info, walkFn, visited, options)
	if err != nil {
		if (!info.IsDir() && !IsSymlink(info)) || !errors.Is(err, filepath.SkipDir) {
			return err
		}
	}

	return nil
}

// isPermissionError checks if an error is a permission-related error.
// This function is specifically designed for post-error analysis to determine
// if an operation failed due to insufficient permissions, allowing the caller
// to decide whether to skip or handle the error gracefully.
//
// Note: This is different from CheckReadPath which proactively checks permissions.
// Using isPermissionError is more efficient as it analyzes existing errors rather
// than making additional system calls.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a permission denied error
	if errors.Is(err, os.ErrPermission) {
		return true
	}

	// Check syscall errors
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errors.Is(errno, syscall.EACCES) || errors.Is(errno, syscall.EPERM)
	}

	return false
}

// IsSymlink checks if a file is a symbolic link.
func IsSymlink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}

// GetAllFilePaths walks the file tree rooted at root and returns a slice of absolute paths
// for all files (not directories) found. It uses the same symbolic link handling and
// permission error handling as SymWalk. By default, empty files (size 0) are skipped
// through SymWalk's SkipEmptyFiles option.
//
// root: the starting path for traversal
// options: configuration options, if nil, DefaultSymWalkOptions() is used
// Returns: slice of absolute file paths and any error encountered.
func GetAllFilePaths(root string, options *SymWalkOptions) ([]string, error) {
	if options == nil {
		options = DefaultSymWalkOptions()
	}

	var filePaths []string

	// Define the walk function that collects file paths
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only collect regular files, not directories
		if !info.IsDir() {
			// Check if we've reached the maximum number of files
			if options.MaxFiles > 0 && len(filePaths) >= options.MaxFiles {
				log.Warnf("GetAllFilePaths: reached maximum file limit (%d), stopping collection at path: %s", options.MaxFiles, path)
				return filepath.SkipDir // Stop traversal
			}

			// Convert to absolute path
			absPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			filePaths = append(filePaths, absPath)
		}

		return nil
	}

	// Walk the directory tree
	err := SymWalk(root, walkFn, options)
	if err != nil {
		return nil, err
	}

	return filePaths, nil
}
