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
	"syscall"
)

// SymWalkOptions contains simple configuration options for SymWalk.
type SymWalkOptions struct {
	// FollowSymlinks when true, follows symbolic links. When false, treats symlinks as regular files.
	FollowSymlinks bool

	// SkipPermissionErrors when true, skips directories/files that cannot be accessed due to permission errors
	// instead of returning an error.
	SkipPermissionErrors bool
}

// DefaultSymWalkOptions returns a default configuration.
func DefaultSymWalkOptions() *SymWalkOptions {
	return &SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
	}
}

// SymWalk walks the file tree rooted at root, calling walkFn for each file or
// directory in the tree, including root. It supports following symbolic links
// safely by preventing infinite loops. The files are walked in lexical order,
// which makes the output deterministic.
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

// readDirNames reads the directory named by dirname and returns
// a sorted list of directory entries.
func readDirNames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// symwalk recursively descends path, calling walkFn.
func symwalk(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Handle symbolic links
	if IsSymlink(info) && options.FollowSymlinks {
		return handleSymlink(path, info, walkFn, visited, options)
	}

	// Call walkFn for current file/directory
	if err := walkFn(path, info, nil); err != nil {
		return err
	}

	// If it's not a directory, we're done
	if !info.IsDir() {
		return nil
	}

	return walkDirectory(path, info, walkFn, visited, options)
}

// handleSymlink processes symbolic links.
func handleSymlink(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Resolve symlink to real path
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
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

// walkDirectory processes directory contents.
func walkDirectory(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool, options *SymWalkOptions) error {
	// Read directory contents in sorted order
	names, err := readDirNames(path)
	if err != nil {
		if options.SkipPermissionErrors && isPermissionError(err) {
			return nil
		}
		return walkFn(path, info, err)
	}

	// Traverse each entry in the directory
	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := os.Lstat(filename)
		//nolint: nestif // This is a common pattern in file walking code, so we keep it for clarity.
		if err != nil {
			if options.SkipPermissionErrors && isPermissionError(err) {
				continue
			}
			if err := walkFn(filename, fileInfo, err); err != nil && !errors.Is(err, filepath.SkipDir) {
				return err
			}
		} else {
			err = symwalk(filename, fileInfo, walkFn, visited, options)
			if err != nil {
				if (!fileInfo.IsDir() && !IsSymlink(fileInfo)) || !errors.Is(err, filepath.SkipDir) {
					return err
				}
			}
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
