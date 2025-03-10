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
	baseFolder := rc.GetBaseFolder()
	dirPath := filepath.Join(baseFolder, ".cos")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			return errors.Wrap(err, "create cache directory failed")
		}
	}

	file := filepath.Join(dirPath, "state.json")
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal record cache failed")
	}

	//nolint: gosec // 0644 is the standard permission for files
	err = os.WriteFile(file, data, 0644)
	if err != nil {
		return errors.Wrap(err, "write record cache failed")
	}
	return nil
}

func (rc *RecordCache) Reload() (*RecordCache, error) {
	baseFolder := rc.GetBaseFolder()
	file := filepath.Join(baseFolder, ".cos", "state.json")
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil, errors.Wrap(err, "record cache file not exist")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, errors.Wrap(err, "read record cache failed")
	}

	err = json.Unmarshal(data, rc)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal record cache failed")
	}
	return rc, nil
}

func (rc *RecordCache) Clean() string {
	baseFolder := rc.GetBaseFolder()
	if utils.CheckReadPath(baseFolder) {
		if utils.DeleteDir(baseFolder) {
			return baseFolder
		}
	}
	return ""
}

func (rc *RecordCache) GetRecordCachePath() string {
	baseFolder := rc.GetBaseFolder()
	return filepath.Join(baseFolder, ".cos", "state.json")
}
