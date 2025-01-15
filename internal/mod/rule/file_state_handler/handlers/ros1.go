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

package handlers

import (
	"os"
	"strings"
	"time"

	"github.com/foxglove/go-rosbag"
	"github.com/pkg/errors"
)

type ros1Handler struct {
	defaultGetFileSize
}

func NewRos1Handler() Interface {
	return &ros1Handler{}
}

// CheckFilePath checks if the file path is supported by the handler.
func (h *ros1Handler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .ros1 extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".ros1")
}

func (h *ros1Handler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	ros1FileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, errors.Errorf("open ros1 file [%s] failed: %v", filePath, err)
	}
	defer ros1FileReader.Close()

	reader, err := rosbag.NewReader(ros1FileReader)
	if err != nil {
		return nil, nil, errors.Errorf("failed to create ros1 reader for ros1 file %s: %v", filePath, err)
	}

	info, err := reader.Info()
	if err != nil {
		return nil, nil, errors.Errorf("failed to get info for ros1 file %s: %v", filePath, err)
	}

	startNano := info.MessageStartTime
	endNano := info.MessageEndTime
	//nolint: gosec // ignore uint64 to int64 conversion
	start := time.Unix(int64(startNano/1e9), int64(startNano%1e9))
	//nolint: gosec // ignore uint64 to int64 conversion
	end := time.Unix(int64(endNano/1e9), int64(endNano%1e9))

	return &start, &end, nil
}
