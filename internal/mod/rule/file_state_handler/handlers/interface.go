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
	"time"
)

// Interface defines the interface for handling different file types.
type Interface interface {
	// CheckFilePath checks if the file path is supported by the handler.
	CheckFilePath(filePath string) bool

	// GetStartTimeEndTime computes the start and end time of the log file.
	GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error)

	// GetFileSize returns the file size.
	GetFileSize(filePath string) (int64, error)
}

// defaultGetFileSize provides default implementations for some methods.
type defaultGetFileSize struct{}

// GetFileSize is a default implementation that can be used by handlers.
func (h *defaultGetFileSize) GetFileSize(filePath string) (int64, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}
