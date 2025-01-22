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

	// id field does not need to be serialized
	Id string `json:"-" yaml:"-"`
}

func (ci *CollectInfo) GetFilePath() string {
	return path.Join(config.GetCollectInfoFolder(), ci.Id+".json")
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

func (ci *CollectInfo) Save() error {
	if ci.Id == "" {
		ci.Id = uuid.New().String()
	}

	data, err := json.MarshalIndent(ci, "", "  ")
	if err != nil {
		return errors.Wrap(err, "marshal collect info")
	}

	//nolint: gosec // 0644 is the standard permission for files
	err = os.WriteFile(ci.GetFilePath(), data, 0644)
	if err != nil {
		return errors.Wrap(err, "write record cache")
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
