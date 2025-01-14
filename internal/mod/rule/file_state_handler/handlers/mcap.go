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

	"github.com/foxglove/mcap/go/mcap"
)

type mcapHandler struct {
	defaultGetFileSize
}

func NewMcapHandler() Interface {
	return &mcapHandler{}
}

// CheckFilePath checks if the file path is supported by the handler
func (h *mcapHandler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .mcap extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".mcap")
}

func (h *mcapHandler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	mcapFileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open mcap file [%s] failed: %w", filePath, err)
	}
	defer mcapFileReader.Close()

	reader, err := mcap.NewReader(mcapFileReader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create mcap reader for mcap file %s: %w", filePath, err)
	}

	info, err := reader.Info()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get info for mcap file %s: %w", filePath, err)
	}

	startNano := info.Statistics.MessageStartTime
	endNano := info.Statistics.MessageEndTime
	start := time.Unix(int64(startNano/1e9), int64(startNano%1e9))
	end := time.Unix(int64(endNano/1e9), int64(endNano%1e9))

	return &start, &end, nil
}
