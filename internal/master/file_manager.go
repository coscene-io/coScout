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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
)

// LimitedReadCloser wraps an io.ReadCloser with a size limit
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

// FileManager manages master-slave file transfer
type FileManager struct {
	client       *Client
	registry     *SlaveRegistry
	cacheDir     string
	downloadedFiles sync.Map // Cache mapping of downloaded file paths
	directStream bool        // Enable direct streaming without caching
}

// NewFileManager creates a file manager
func NewFileManager(client *Client, registry *SlaveRegistry, cacheDir string) *FileManager {
	return &FileManager{
		client:       client,
		registry:     registry,
		cacheDir:     cacheDir,
		directStream: false, // Default: use caching
	}
}

// NewFileManagerWithDirectStream creates a file manager with direct streaming
func NewFileManagerWithDirectStream(client *Client, registry *SlaveRegistry, cacheDir string, directStream bool) *FileManager {
	return &FileManager{
		client:       client,
		registry:     registry,
		cacheDir:     cacheDir,
		directStream: directStream,
	}
}

// GetFileReader gets file reader, automatically handles local/remote files
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
	if fileInfo.Size > 0 {
		log.Infof("Limiting file read to %d bytes for %s", fileInfo.Size, fileInfo.Path)
		reader = NewLimitedReadCloser(reader, fileInfo.Size)
	}
	
	return reader, nil
}

// getLocalFileReader gets local file reader
func (fm *FileManager) getLocalFileReader(path string) (io.ReadCloser, error) {
	if !utils.CheckReadPath(path) {
		return nil, fmt.Errorf("file %s is not accessible", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open local file %s: %w", path, err)
	}

	return file, nil
}

// getSlaveFileReader gets slave file reader
func (fm *FileManager) getSlaveFileReader(ctx context.Context, fileInfo model.FileInfo) (io.ReadCloser, error) {
	// Parse slave file path: slave://slaveID/absolutePath
	slaveID, remotePath, err := ParseSlaveFilePath(fileInfo.Path)
	if err != nil {
		return nil, fmt.Errorf("parse slave file path: %w", err)
	}

	// Get slave information
	slave, exists := fm.registry.GetSlave(slaveID)
	if !exists {
		return nil, fmt.Errorf("slave %s not found", slaveID)
	}

	if !slave.IsOnline() {
		return nil, fmt.Errorf("slave %s is not online", slaveID)
	}

	// Direct streaming mode: bypass cache for immediate upload scenarios
	if fm.directStream {
		log.Infof("Direct streaming file %s from slave %s", remotePath, slave.ID)
		return fm.client.DownloadSlaveFileWithSize(ctx, slave, remotePath, fileInfo.Size)
	}

	// Cache mode: check existing cache first
	if localPath, exists := fm.downloadedFiles.Load(fileInfo.Path); exists {
		if localPathStr, ok := localPath.(string); ok {
			if utils.CheckReadPath(localPathStr) {
				log.Debugf("Using cached file: %s", localPathStr)
				return fm.getLocalFileReader(localPathStr)
			}
			// Cached file doesn't exist, remove cache record
			fm.downloadedFiles.Delete(fileInfo.Path)
		}
	}

	// Download file to cache directory
	return fm.downloadSlaveFile(ctx, slave, remotePath, fileInfo)
}

// downloadSlaveFile downloads slave file
func (fm *FileManager) downloadSlaveFile(ctx context.Context, slave *SlaveInfo, remotePath string, fileInfo model.FileInfo) (io.ReadCloser, error) {
	log.Infof("Downloading file %s from slave %s", remotePath, slave.ID)

	// Create cache directory
	if err := fm.ensureCacheDir(); err != nil {
		return nil, fmt.Errorf("ensure cache dir: %w", err)
	}

	// Generate cache file path organized by slave ID
	slaveCacheDir := filepath.Join(fm.cacheDir, slave.ID)
	if err := os.MkdirAll(slaveCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create slave cache dir: %w", err)
	}
	
	cacheFileName := fmt.Sprintf("%s_%d", filepath.Base(remotePath), time.Now().Unix())
	cacheFilePath := filepath.Join(slaveCacheDir, cacheFileName)

	// Download file from slave with size limit
	reader, err := fm.client.DownloadSlaveFileWithSize(ctx, slave, remotePath, fileInfo.Size)
	if err != nil {
		return nil, fmt.Errorf("download from slave %s: %w", slave.ID, err)
	}

	// Create cache file
	cacheFile, err := os.Create(cacheFilePath)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("create cache file: %w", err)
	}

	// Write data to cache file
	_, err = io.Copy(cacheFile, reader)
	reader.Close()
	cacheFile.Close()

	if err != nil {
		os.Remove(cacheFilePath) // Clean up partially downloaded file
		return nil, fmt.Errorf("copy file data: %w", err)
	}

	// Cache file path mapping
	fm.downloadedFiles.Store(fileInfo.Path, cacheFilePath)
	log.Infof("File %s downloaded and cached as %s", remotePath, cacheFilePath)

	// Return cache file reader
	return fm.getLocalFileReader(cacheFilePath)
}

// ensureCacheDir ensures cache directory exists
func (fm *FileManager) ensureCacheDir() error {
	if fm.cacheDir == "" {
		return fmt.Errorf("cache directory not configured")
	}

	return os.MkdirAll(fm.cacheDir, 0755)
}

// CleanupCache cleans up cache
func (fm *FileManager) CleanupCache() error {
	if fm.cacheDir == "" {
		return nil
	}

	log.Infof("Cleaning up cache directory: %s", fm.cacheDir)
	
	// Iterate and delete cache files
	var deletedCount int
	fm.downloadedFiles.Range(func(key, value interface{}) bool {
		if localPath, ok := value.(string); ok {
			if err := os.Remove(localPath); err != nil {
				log.Warnf("Failed to remove cached file %s: %v", localPath, err)
			} else {
				deletedCount++
			}
		}
		fm.downloadedFiles.Delete(key)
		return true
	})

	log.Infof("Cleaned up %d cached files", deletedCount)
	return nil
}

// CleanupFileCache cleans up cache for a specific file (useful after upload)
func (fm *FileManager) CleanupFileCache(filePath string) error {
	if localPath, exists := fm.downloadedFiles.Load(filePath); exists {
		if localPathStr, ok := localPath.(string); ok {
			if err := os.Remove(localPathStr); err != nil {
				log.Warnf("Failed to remove cached file %s: %v", localPathStr, err)
				return err
			}
			log.Infof("Removed cached file after upload: %s", localPathStr)
		}
		fm.downloadedFiles.Delete(filePath)
	}
	return nil
}

// CleanupSlaveCache cleans up all cache for a specific slave
func (fm *FileManager) CleanupSlaveCache(slaveID string) error {
	if fm.cacheDir == "" {
		return nil
	}
	
	slaveCacheDir := filepath.Join(fm.cacheDir, slaveID)
	if _, err := os.Stat(slaveCacheDir); os.IsNotExist(err) {
		return nil // Directory doesn't exist, nothing to clean
	}
	
	log.Infof("Cleaning up cache for slave %s: %s", slaveID, slaveCacheDir)
	
	// Remove the entire slave cache directory
	if err := os.RemoveAll(slaveCacheDir); err != nil {
		log.Warnf("Failed to remove slave cache directory %s: %v", slaveCacheDir, err)
		return err
	}
	
	// Remove entries from downloadedFiles map
	fm.downloadedFiles.Range(func(key, value interface{}) bool {
		if pathStr, ok := key.(string); ok {
			if sid, _, err := ParseSlaveFilePath(pathStr); err == nil && sid == slaveID {
				fm.downloadedFiles.Delete(key)
			}
		}
		return true
	})
	
	log.Infof("Cleaned up cache for slave %s", slaveID)
	return nil
}

// GetCacheStats gets cache statistics
func (fm *FileManager) GetCacheStats() map[string]interface{} {
	stats := make(map[string]interface{})
	
	var totalFiles int
	var totalSize int64
	
	fm.downloadedFiles.Range(func(key, value interface{}) bool {
		totalFiles++
		if localPath, ok := value.(string); ok {
			if info, err := os.Stat(localPath); err == nil {
				totalSize += info.Size()
			}
		}
		return true
	})

	stats["total_files"] = totalFiles
	stats["total_size"] = totalSize
	stats["cache_dir"] = fm.cacheDir

	return stats
}

// IsSlaveFile checks if it's a slave file (format: slave://slaveID/absolutePath)
func IsSlaveFile(path string) bool {
	return strings.HasPrefix(path, "slave://")
}

// ParseSlaveFilePath parses slave file path (format: slave://slaveID/absolutePath)
func ParseSlaveFilePath(path string) (slaveID, remotePath string, err error) {
	if !strings.HasPrefix(path, "slave://") {
		return "", "", fmt.Errorf("invalid slave file path format: %s (expected slave://slaveID/path)", path)
	}

	// Remove "slave://" prefix
	pathWithoutScheme := strings.TrimPrefix(path, "slave://")
	
	// Find first "/" to separate slaveID and path
	slashIndex := strings.Index(pathWithoutScheme, "/")
	if slashIndex == -1 {
		return "", "", fmt.Errorf("invalid slave file path format: %s (missing path after slaveID)", path)
	}

	slaveID = pathWithoutScheme[:slashIndex]
	remotePath = pathWithoutScheme[slashIndex:] // Keep leading "/"

	if slaveID == "" {
		return "", "", fmt.Errorf("empty slave ID in path: %s", path)
	}

	if remotePath == "/" || remotePath == "" {
		return "", "", fmt.Errorf("empty remote path in: %s", path)
	}

	// Validate slave ID format
	if !IsValidSlaveID(slaveID) {
		return "", "", fmt.Errorf("invalid slave ID format: %s (must be 16 alphanumeric characters)", slaveID)
	}

	return slaveID, remotePath, nil
}

// FormatSlaveFilePath formats slave file path (format: slave://slaveID/absolutePath)
func FormatSlaveFilePath(slaveID, remotePath string) string {
	// Validate slave ID format
	if !IsValidSlaveID(slaveID) {
		log.Warnf("Invalid slave ID format: %s", slaveID)
	}
	
	// Ensure remotePath starts with "/"
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	return fmt.Sprintf("slave://%s%s", slaveID, remotePath)
}

// IsValidSlaveID validates if slave ID meets standard (16 characters, alphanumeric only)
func IsValidSlaveID(slaveID string) bool {
	if len(slaveID) != 16 {
		return false
	}
	
	// Check if contains only alphanumeric characters
	for _, char := range slaveID {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return false
		}
	}
	
	return true
}
