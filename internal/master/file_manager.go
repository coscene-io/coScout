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

package master

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Static errors for better error handling.
var (
	ErrNotSlaveFilePath     = errors.New("not a slave file path")
	ErrInvalidSlaveFilePath = errors.New("invalid slave file path format")
	ErrSlaveNotFound        = errors.New("slave not found")
	ErrSlaveNotOnline       = errors.New("slave not online")
)

// LimitedReadCloser wraps an io.ReadCloser with a size limit.
type LimitedReadCloser struct {
	reader io.ReadCloser
	limit  int64
	read   int64
}

func NewLimitedReadCloser(reader io.ReadCloser, limit int64) *LimitedReadCloser {
	return &LimitedReadCloser{
		reader: reader,
		limit:  limit,
		read:   0,
	}
}

func (lrc *LimitedReadCloser) Read(p []byte) (n int, err error) {
	if lrc.read >= lrc.limit {
		return 0, io.EOF
	}
	remaining := lrc.limit - lrc.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err = lrc.reader.Read(p)
	lrc.read += int64(n)
	if lrc.read >= lrc.limit {
		err = io.EOF
	}
	return n, err
}

func (lrc *LimitedReadCloser) Close() error {
	return lrc.reader.Close()
}

// FileManager manages master-slave file transfer.
type FileManager struct {
	client   *Client
	registry *SlaveRegistry
}

// NewFileManager creates a file manager.
func NewFileManager(client *Client, registry *SlaveRegistry) *FileManager {
	return &FileManager{
		client:   client,
		registry: registry,
	}
}

// GetFileReader gets file reader, automatically handles local/remote files.
func (fm *FileManager) GetFileReader(ctx context.Context, fileInfo model.FileInfo) (io.ReadCloser, error) {
	var reader io.ReadCloser
	var err error

	// Check if it's a slave file (path format: slave://slaveID/absolutePath)
	if IsSlaveFile(fileInfo.Path) {
		reader, err = fm.getSlaveFileReader(ctx, fileInfo)
	} else {
		// Local file
		reader, err = fm.getLocalFileReader(fileInfo.Path)
	}

	if err != nil {
		return nil, err
	}

	// If file size is specified and > 0, limit reading to that size
	// Note: For slave files being copied to local cache, we want to read the complete file
	// to avoid issues with files being modified on the slave side
	if fileInfo.Size > 0 && !IsSlaveFile(fileInfo.Path) {
		log.Infof("Limiting file read to %d bytes for %s", fileInfo.Size, fileInfo.Path)
		reader = NewLimitedReadCloser(reader, fileInfo.Size)
	} else if IsSlaveFile(fileInfo.Path) {
		log.Infof("Reading complete slave file %s (no size limit)", fileInfo.Path)
	}

	return reader, nil
}

// IsSlaveFile checks if the file path is a slave file.
func IsSlaveFile(filePath string) bool {
	return strings.HasPrefix(filePath, "slave://")
}

// IsValidSlaveID validates if a slave ID is in the correct format (16 hex characters).
func IsValidSlaveID(slaveID string) bool {
	if len(slaveID) != 16 {
		return false
	}

	// Check if all characters are valid hex characters
	for _, char := range slaveID {
		if !((char >= '0' && char <= '9') ||
			(char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F')) {
			return false
		}
	}

	return true
}

// ParseSlaveFilePath parses slave file path: slave://slaveID/absolutePath
func ParseSlaveFilePath(filePath string) (slaveID, remotePath string, err error) {
	if !IsSlaveFile(filePath) {
		return "", "", errors.Wrapf(ErrNotSlaveFilePath, "path: %s", filePath)
	}

	// Remove "slave://" prefix
	pathWithoutPrefix := strings.TrimPrefix(filePath, "slave://")

	// Split by first "/" to get slaveID and remotePath
	parts := strings.SplitN(pathWithoutPrefix, "/", 2)
	if len(parts) != 2 {
		return "", "", errors.Wrapf(ErrInvalidSlaveFilePath, "path: %s", filePath)
	}

	slaveID = parts[0]
	remotePath = "/" + parts[1] // Ensure remotePath starts with "/"

	return slaveID, remotePath, nil
}

// getLocalFileReader gets local file reader.
func (fm *FileManager) getLocalFileReader(filePath string) (io.ReadCloser, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}

	return file, nil
}

// getSlaveFileReader gets slave file reader.
func (fm *FileManager) getSlaveFileReader(ctx context.Context, fileInfo model.FileInfo) (io.ReadCloser, error) {
	// Parse slave file path: slave://slaveID/absolutePath
	slaveID, remotePath, err := ParseSlaveFilePath(fileInfo.Path)
	if err != nil {
		return nil, fmt.Errorf("parse slave file path: %w", err)
	}

	// Get slave information
	slave, exists := fm.registry.GetSlave(slaveID)
	if !exists {
		return nil, errors.Wrapf(ErrSlaveNotFound, "slave ID: %s", slaveID)
	}

	if !slave.IsOnline() {
		return nil, errors.Wrapf(ErrSlaveNotOnline, "slave ID: %s", slaveID)
	}

	// Direct streaming from slave (no size limit for complete file copy)
	log.Infof("Streaming file %s from slave %s", remotePath, slave.ID)
	return fm.client.DownloadSlaveFileWithSize(ctx, slave, remotePath, 0) // 0 means no size limit
}
