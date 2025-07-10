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

package register

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type FileModRegister struct {
	conf config.FileModRegisterConfig
}

func NewFileModRegister(conf interface{}) ModRegister {
	bytes, err := json.Marshal(conf)
	if err != nil {
		log.Errorf("Unable to marshal mod register config: %v", err)
		return nil
	}

	reConf := config.FileModRegisterConfig{}
	if err := json.Unmarshal(bytes, &reConf); err != nil {
		log.Errorf("Unable to unmarshal mod register config: %v", err)
		return nil
	}

	return &FileModRegister{
		conf: reConf,
	}
}

func (f *FileModRegister) GetDevice() *openDpsV1alpha1Resource.Device {
	if f.conf.SnFile == "" {
		return nil
	}

	if !utils.CheckReadPath(f.conf.SnFile) {
		log.Warnf("local device file %s not exist", f.conf.SnFile)
		return nil
	}

	// if yaml or json file, read the specific field from the file and return the device
	if strings.HasSuffix(f.conf.SnFile, ".yaml") ||
		strings.HasSuffix(f.conf.SnFile, ".yml") ||
		strings.HasSuffix(f.conf.SnFile, ".json") {
		deviceID, err := getDeviceFromStructuredFile(f.conf.SnFile, f.conf.SnField)
		if err != nil {
			log.Errorf("failed to get device from structured file: %v", err)
			return nil
		}

		if deviceID == "" {
			log.Errorf("get empty device id from structured file: %s", f.conf.SnFile)
			return nil
		}

		return &openDpsV1alpha1Resource.Device{
			DisplayName:  deviceID,
			SerialNumber: deviceID,
			Description:  deviceID,
			Tags:         core.GetCustomTags(),
		}
	}

	// other files, just read the first line and return the device
	deviceID, err := getDeviceFromText(f.conf.SnFile)
	if err != nil {
		log.Errorf("failed to get device from text file: %v", err)
		return nil
	}

	if deviceID == "" {
		log.Errorf("get empty device id from text file: %s", f.conf.SnFile)
		return nil
	}

	return &openDpsV1alpha1Resource.Device{
		DisplayName:  deviceID,
		SerialNumber: deviceID,
		Description:  deviceID,
		Tags:         core.GetCustomTags(),
	}
}

func getDeviceFromStructuredFile(snFile, snField string) (string, error) {
	data, err := os.ReadFile(snFile)
	if err != nil {
		return "", errors.Wrap(err, "failed to read device file")
	}

	var result map[string]interface{}
	if strings.HasSuffix(snFile, ".json") {
		err = json.Unmarshal(data, &result)
	} else { // yaml file
		err = yaml.Unmarshal(data, &result)
	}

	if err != nil {
		return "", errors.Wrap(err, "failed to parse device file")
	}

	deviceID, ok := result[snField]
	if !ok || deviceID == nil {
		return "", errors.Errorf("field '%s' not found or empty in file %s", snField, snFile)
	}

	deviceIDStr := fmt.Sprintf("%v", deviceID)
	deviceIDStr = strings.TrimSpace(deviceIDStr)
	if deviceIDStr == "" {
		return "", errors.Errorf("field '%s' contains empty value in file %s", snField, snFile)
	}

	return deviceIDStr, nil
}

func getDeviceFromText(snFile string) (string, error) {
	data, err := os.ReadFile(snFile)
	if err != nil {
		return "", errors.Wrap(err, "failed to read device file")
	}

	return strings.TrimSpace(string(data)), nil
}
