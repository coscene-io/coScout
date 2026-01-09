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
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/djherbis/times"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// CheckReadPath checks if a path is readable.
// Uses Lstat for better performance and then Access to check actual readability.
func CheckReadPath(path string) bool {
	if path == "" {
		return false
	}

	// Use Lstat for better performance - it doesn't follow symlinks
	// but unix.Access will still check the final target
	_, err := os.Lstat(path)
	if err != nil {
		return false
	}

	// unix.Access follows symlinks automatically, so it checks the final target
	err = unix.Access(path, unix.R_OK)
	return err == nil
}

func DeleteDir(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return true
	}

	err := os.RemoveAll(dir)
	return err == nil
}

func GetParentFolder(path string) string {
	return filepath.Dir(path)
}

func CalSha256AndSize(absPath string, sizeToRead int64) (string, int64, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", 0, errors.Wrapf(err, "open file")
	}
	defer func(osFile *os.File) {
		err := osFile.Close()
		if err != nil {
			log.Error(err)
		}
	}(f)

	fileInfo, err := f.Stat()
	if err != nil {
		return "", 0, errors.Wrapf(err, "stat file")
	}

	fileSize := fileInfo.Size()

	// If sizeToRead is 0 or greater than file size, read the entire file
	if sizeToRead <= 0 || sizeToRead > fileSize {
		sizeToRead = fileSize
	}

	hash := sha256.New()

	// Use io.LimitReader to read only the specified amount
	limitedReader := io.LimitReader(f, sizeToRead)
	if _, err := io.Copy(hash, limitedReader); err != nil {
		return "", 0, errors.Wrapf(err, "read file")
	}

	return hex.EncodeToString(hash.Sum(nil)), sizeToRead, nil
}

// GetFileSize returns the size of the file at the given path.
// For symbolic links, it follows the link and returns the target file size.
func GetFileSize(filepath string) (int64, error) {
	fi, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// GetFileSizeNoFollow returns the size of the file at the given path.
// For symbolic links, it returns the size of the link itself, not the target.
func GetFileSizeNoFollow(filepath string) (int64, error) {
	fi, err := os.Lstat(filepath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func isSymlinkPath(filepath string) bool {
	if filepath == "" {
		return false
	}

	info, err := os.Lstat(filepath)
	if err != nil {
		log.Errorf("lstat filepath: %s,  error: %v", filepath, err)
		return false
	}

	return info.Mode()&os.ModeSymlink != 0
}

func getRealPath(path string) (string, error) {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.Wrapf(err, "eval symlinks for path: %s", path)
	}
	return realPath, nil
}

func GetRealFileInfo(path string) (string, os.FileInfo, error) {
	if path == "" {
		return "", nil, errors.New("empty path")
	}

	isSymlink := isSymlinkPath(path)
	if isSymlink {
		realPath, err := getRealPath(path)
		if err != nil {
			return "", nil, errors.Wrapf(err, "get real path for symlink: %s", path)
		}
		path = realPath
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", nil, errors.Wrapf(err, "stat real path: %s", path)
	}
	return path, info, nil
}

func HasBirthTime(paths []string) bool {
	for _, tmpPath := range paths {
		canRead := CheckReadPath(tmpPath)
		if !canRead {
			continue
		}

		stat, err := times.Stat(tmpPath)
		if err != nil {
			continue
		}

		if stat.HasBirthTime() {
			return true
		}
	}
	return false
}
