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

package model

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type FileInfo struct {
	Path     string `json:"path" yaml:"path"`
	Size     int64  `json:"size" yaml:"size"`
	Sha256   string `json:"sha256" yaml:"sha256"`
	FileName string `json:"file_name" yaml:"file_name"`
}

type Moment struct {
	Name        string `json:"name" yaml:"name"`
	IsNew       bool   `json:"is_new" yaml:"is_new"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	// Timestamp seconds
	Timestamp float64 `json:"timestamp" yaml:"timestamp"`
	// Duration seconds
	Duration float64                `json:"duration" yaml:"duration"`
	Metadata map[string]string      `json:"metadata" yaml:"metadata"`
	Task     Task                   `json:"task" yaml:"task"`
	Event    map[string]interface{} `json:"event" yaml:"event"`
	Code     string                 `json:"code" yaml:"code"`
	RuleName string                 `json:"rule_name" yaml:"rule_name"`
}

type Task struct {
	ShouldCreate bool   `json:"should_create" yaml:"should_create"`
	Name         string `json:"name" yaml:"name"`
	Title        string `json:"title" yaml:"title"`
	Description  string `json:"description" yaml:"description"`
	RecordName   string `json:"record_name" yaml:"record_name"`
	Assignee     string `json:"assignee" yaml:"assignee"`
	SyncTask     bool   `json:"sync_task" yaml:"sync_task"`
}

type RecordCache struct {
	mu        sync.RWMutex
	cachePath string

	Uploaded    bool   `json:"uploaded" yaml:"uploaded"`
	Skipped     bool   `json:"skipped" yaml:"skipped"`
	EventCode   string `json:"event_code" yaml:"event_code"`
	ProjectName string `json:"project_name" yaml:"project_name"`

	// Timestamp milliseconds
	Timestamp int64                  `json:"timestamp" yaml:"timestamp"`
	Labels    []string               `json:"labels" yaml:"labels"`
	Record    map[string]interface{} `json:"record" yaml:"record"`
	Moments   []Moment               `json:"moments" yaml:"moments"`

	UploadTask    map[string]interface{} `json:"task" yaml:"task"`
	DiagnosisTask map[string]interface{} `json:"diagnosis_task" yaml:"diagnosis_task"`

	// key is absolute file path, value is file info
	OriginalFiles     map[string]FileInfo `json:"files" yaml:"files"`
	UploadedFilePaths []string            `json:"uploaded_filepaths" yaml:"uploaded_filepaths"`

	// randomPostfix is used to avoid conflict when creating cache folder
	RandomPostfix string `json:"random_postfix" yaml:"random_postfix"`
}

func (rc *RecordCache) GetBaseFolder() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.cachePath != "" {
		return recordCacheBaseFolder(rc.cachePath)
	}
	return rc.getBaseFolder()
}

func (rc *RecordCache) getBaseFolder() string {
	baseFolder := config.GetRecordCacheFolder()

	seconds := rc.Timestamp / 1000
	milliseconds := rc.Timestamp % 1000
	if rc.RandomPostfix == "" {
		rc.RandomPostfix = uuid.New().String()
	}
	dirName := time.Unix(seconds, 0).UTC().Format("2006-01-02-15-04-05") + "_" + strconv.Itoa(int(milliseconds)) + "_" + rc.RandomPostfix
	return path.Join(baseFolder, dirName)
}

func (rc *RecordCache) Save() error {
	// use write lock to protect concurrent writes
	rc.mu.Lock()
	defer rc.mu.Unlock()

	file := rc.recordCachePathLocked()
	rc.cachePath = file
	unlock := recordCacheFileLocks.Lock(file)
	defer unlock()

	return writeRecordCacheFile(file, rc)
}

func LoadRecordCache(recordCachePath string) (*RecordCache, error) {
	recordCachePath = normalizeRecordCachePath(recordCachePath)
	if recordCachePath == "" {
		return nil, errors.New("record cache path is empty")
	}
	unlock := recordCacheFileLocks.Lock(recordCachePath)
	defer unlock()

	return loadRecordCacheFile(recordCachePath)
}

// UpdateRecordCache serializes a full read-modify-write cycle for one state.json
// path. The update function must only mutate the provided cache object; calling
// Save, Reload, Update, or UpdateRecordCache from inside it would try to re-enter
// the same file lock.
func UpdateRecordCache(recordCachePath string, update func(*RecordCache) error) (*RecordCache, error) {
	if update == nil {
		return nil, errors.New("record cache update func is nil")
	}

	recordCachePath = normalizeRecordCachePath(recordCachePath)
	if recordCachePath == "" {
		return nil, errors.New("record cache path is empty")
	}
	unlock := recordCacheFileLocks.Lock(recordCachePath)
	defer unlock()

	rc, err := loadRecordCacheFile(recordCachePath)
	if err != nil {
		return nil, err
	}

	if err := update(rc); err != nil {
		return nil, err
	}

	if err := writeRecordCacheFile(recordCachePath, rc); err != nil {
		return nil, err
	}
	return rc, nil
}

func (rc *RecordCache) Update(update func(*RecordCache) error) (*RecordCache, error) {
	return UpdateRecordCache(rc.GetRecordCachePath(), update)
}

func (rc *RecordCache) Reload() (*RecordCache, error) {
	// use write lock to protect concurrent access
	rc.mu.Lock()
	defer rc.mu.Unlock()

	file := rc.recordCachePathLocked()
	unlock := recordCacheFileLocks.Lock(file)
	defer unlock()

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil, errors.Wrap(err, "record cache file not exist")
	}

	reloaded, err := loadRecordCacheFile(file)
	if err != nil {
		return nil, err
	}
	rc.copyFrom(reloaded)
	return rc, nil
}

func (rc *RecordCache) Clean() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	file := rc.recordCachePathLocked()
	rc.cachePath = file
	baseFolder := recordCacheBaseFolder(file)
	unlock := recordCacheFileLocks.Lock(file)
	defer unlock()

	if utils.CheckReadPath(baseFolder) {
		if utils.DeleteDir(baseFolder) {
			return baseFolder
		}
	}
	return ""
}

func (rc *RecordCache) GetRecordCachePath() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.cachePath != "" {
		return rc.cachePath
	}
	baseFolder := rc.getBaseFolder()
	rc.cachePath = filepath.Join(baseFolder, ".cos", "state.json")
	return rc.cachePath
}

func recordCacheBaseFolder(recordCachePath string) string {
	return filepath.Dir(filepath.Dir(recordCachePath))
}

func normalizeRecordCachePath(recordCachePath string) string {
	recordCachePath = strings.TrimSpace(recordCachePath)
	if recordCachePath == "" {
		return ""
	}
	return filepath.Clean(recordCachePath)
}

func (rc *RecordCache) recordCachePathLocked() string {
	if rc.cachePath != "" {
		return rc.cachePath
	}
	baseFolder := rc.getBaseFolder()
	return filepath.Join(baseFolder, ".cos", "state.json")
}

func (rc *RecordCache) copyFrom(other *RecordCache) {
	rc.Uploaded = other.Uploaded
	rc.Skipped = other.Skipped
	rc.EventCode = other.EventCode
	rc.ProjectName = other.ProjectName
	rc.Timestamp = other.Timestamp
	rc.Labels = other.Labels
	rc.Record = other.Record
	rc.Moments = other.Moments
	rc.UploadTask = other.UploadTask
	rc.DiagnosisTask = other.DiagnosisTask
	rc.OriginalFiles = other.OriginalFiles
	rc.UploadedFilePaths = other.UploadedFilePaths
	rc.RandomPostfix = other.RandomPostfix
	rc.cachePath = other.cachePath
}

func loadRecordCacheFile(recordCachePath string) (*RecordCache, error) {
	recordCachePath = normalizeRecordCachePath(recordCachePath)
	if recordCachePath == "" {
		return nil, errors.New("record cache path is empty")
	}
	if _, err := os.Stat(recordCachePath); os.IsNotExist(err) {
		return nil, errors.Wrap(err, "record cache file not exist")
	}

	data, err := os.ReadFile(recordCachePath)
	if err != nil {
		return nil, errors.Wrap(err, "read record cache failed")
	}

	rc := &RecordCache{}
	if err := json.Unmarshal(data, rc); err != nil {
		return nil, errors.Wrap(err, "unmarshal record cache failed")
	}
	rc.cachePath = filepath.Clean(recordCachePath)
	return rc, nil
}

func writeRecordCacheFile(recordCachePath string, rc *RecordCache) error {
	recordCachePath = normalizeRecordCachePath(recordCachePath)
	if recordCachePath == "" {
		return errors.New("record cache path is empty")
	}
	dirPath := filepath.Dir(recordCachePath)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return errors.Wrap(err, "create cache directory failed")
		}
	}

	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal record cache failed")
	}

	tmpFile, err := os.CreateTemp(dirPath, ".state-*.tmp")
	if err != nil {
		return errors.Wrap(err, "create temporary record cache file failed")
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return errors.Wrap(err, "write record cache failed")
	}
	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		return errors.Wrap(err, "chmod record cache failed")
	}
	if err := tmpFile.Close(); err != nil {
		return errors.Wrap(err, "close record cache failed")
	}
	if err := os.Rename(tmpPath, recordCachePath); err != nil {
		return errors.Wrap(err, "replace record cache failed")
	}

	return nil
}
