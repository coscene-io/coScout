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

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func CheckReadPath(path string) bool {
	if path == "" {
		return false
	}

	_, err := os.Stat(path)
	if err != nil {
		return false
	}

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

func GetFileSize(filepath string) (int64, error) {
	fi, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
