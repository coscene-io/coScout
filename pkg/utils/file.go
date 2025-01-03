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
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"path/filepath"
)

func CheckReadPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Mode().Perm()&0444 == 0444 {
		return true
	}
	return false
}

func DeleteDir(dir string) bool {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return true
	}

	err := os.RemoveAll(dir)
	if err != nil {
		return false
	}
	return true
}

func GetParentFolder(path string) string {
	return filepath.Dir(path)
}

func CalSha256AndSize(absPath string, sizeToRead int64) (string, int64, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", 0, errors.Wrapf(err, "open file")
	}
	defer func(f *os.File) {
		err := f.Close()
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

	h := sha256.New()

	// Use io.LimitReader to read only the specified amount
	limitedReader := io.LimitReader(f, sizeToRead)
	if _, err := io.Copy(h, limitedReader); err != nil {
		return "", 0, errors.Wrapf(err, "read file")
	}

	return fmt.Sprintf("%x", h.Sum(nil)), sizeToRead, nil
}

func GetFileSize(filepath string) (int64, error) {
	fi, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
