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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/log_reader"
)

type logHandler struct {
	defaultGetFileSize
}

func NewLogHandler() Interface {
	return &logHandler{}
}

// CheckFilePath checks if the file path is supported by the handler
func (h *logHandler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .log extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".log")
}

func (h *logHandler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	logFileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file [%s] failed: %w", filePath, err)
	}
	defer logFileReader.Close()

	reader, err := log_reader.NewLogReader(logFileReader, filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log reader for log file %s: %w", filePath, err)
	}

	startTime, err := reader.GetStartTimestamp()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get start timestamp for log file %s: %w", filePath, err)
	}

	endTime, err := reader.GetEndTimestamp()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get end timestamp for log file %s: %w", filePath, err)
	}

	return startTime, endTime, nil
}
