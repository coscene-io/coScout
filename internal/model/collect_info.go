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

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type CollectInfoCut struct {
	ExtraFiles []string `json:"extra_files" yaml:"extra_files"`
	Start      int64    `json:"start" yaml:"start"` // timestamp in seconds
	End        int64    `json:"end" yaml:"end"`     // timestamp in seconds
	WhiteList  []string `json:"white_list" yaml:"white_list"`
}

type CollectInfoMoment struct {
	Title        string            `json:"title" yaml:"title"`
	Description  string            `json:"description" yaml:"description"`
	Timestamp    float64           `json:"timestamp" yaml:"timestamp"`
	StartTime    float64           `json:"start_time" yaml:"start_time"`
	CustomFields map[string]string `json:"custom_fields" yaml:"custom_fields"`
	Code         string            `json:"code" yaml:"code"`
	CreateTask   bool              `json:"create_task" yaml:"create_task"`
	SyncTask     bool              `json:"sync_task" yaml:"sync_task"`
	AssignTo     string            `json:"assign_to" yaml:"assign_to"` //todo: change to assignee
}

type CollectInfo struct {
	ProjectName   string                 `json:"project_name" yaml:"project_name"`
	Record        map[string]interface{} `json:"record" yaml:"record"`
	Labels        []string               `json:"labels" yaml:"labels"`
	DiagnosisTask map[string]interface{} `json:"diagnosis_task" yaml:"diagnosis_task"`
	Cut           *CollectInfoCut        `json:"cut" yaml:"cut"`
	Moments       []CollectInfoMoment    `json:"moments" yaml:"moments"`
	Skip          bool                   `json:"skip" yaml:"skip"`

	// id field does not need to be serialized
	Id string `json:"-" yaml:"-"`
}

func (ci *CollectInfo) GetFilePath() string {
	return path.Join(config.GetCollectInfoFolder(), ci.Id+".json")
}

func (ci *CollectInfo) GetDraftFilePath() string {
	return ci.GetFilePath() + ".draft"
}

func (ci *CollectInfo) Load(id string) error {
	ci.Id = id
	data, err := os.ReadFile(ci.GetFilePath())
	if err != nil {
		return errors.Wrap(err, "read record cache")
	}

	err = json.Unmarshal(data, ci)
	if err != nil {
		return errors.Wrap(err, "unmarshal collect info")
	}
	return nil
}

func (ci *CollectInfo) LoadDraft(id string) error {
	ci.Id = id
	return ci.loadFromFile(ci.GetDraftFilePath())
}

func (ci *CollectInfo) Save() error {
	ci.ensureID()
	return ci.saveToFile(ci.GetFilePath())
}

func (ci *CollectInfo) SaveDraft() error {
	ci.ensureID()
	return ci.saveToFile(ci.GetDraftFilePath())
}

func (ci *CollectInfo) ensureID() {
	if ci.Id == "" {
		ci.Id = uuid.New().String()
	}
}

func (ci *CollectInfo) loadFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return errors.Wrap(err, "read collect info")
	}

	err = json.Unmarshal(data, ci)
	if err != nil {
		return errors.Wrap(err, "unmarshal collect info")
	}
	return nil
}

func (ci *CollectInfo) saveToFile(filePath string) error {
	data, err := json.MarshalIndent(ci, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal collect info")
	}

	dirPath := filepath.Dir(filePath)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return errors.Wrap(err, "create collect info directory failed")
		}
	}

	tmpFile, err := os.CreateTemp(dirPath, ".collect-info-*.tmp")
	if err != nil {
		return errors.Wrap(err, "create temporary collect info file failed")
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return errors.Wrap(err, "write collect info failed")
	}
	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return errors.Wrap(err, "chmod collect info failed")
	}
	if err := tmpFile.Close(); err != nil {
		return errors.Wrap(err, "close collect info failed")
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return errors.Wrap(err, "replace collect info failed")
	}
	return nil
}

func PublishCollectInfo(id string) error {
	collectInfo := &CollectInfo{Id: id}
	if err := collectInfo.LoadDraft(id); err != nil {
		return err
	}
	if collectInfo.Cut == nil {
		return errors.New("collect info cut is nil")
	}
	if err := os.Rename(collectInfo.GetDraftFilePath(), collectInfo.GetFilePath()); err != nil {
		return errors.Wrap(err, "publish collect info")
	}
	return nil
}

func (ci *CollectInfo) Clean() string {
	filePath := ci.GetFilePath()
	if utils.CheckReadPath(filePath) {
		if err := os.Remove(filePath); err == nil {
			return filePath
		}
	}

	return ""
}
