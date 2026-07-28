// Copyright 2026 coScene
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

package slave

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/pkg/upload"
)

func TestScanFilesRejectsInvalidWindowBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	files, err := (&Server{}).scanFiles("invalid", []string{missingPath}, []string{missingPath}, 2, 1)

	if !errors.Is(err, upload.ErrInvalidTimeWindow) {
		t.Fatalf("error = %v, want ErrInvalidTimeWindow", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
}

func TestScanFilesByContentDefersFutureWindowBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	now := time.Now()
	files, err := (&Server{}).scanFilesByContent(
		"future",
		[]string{missingPath},
		[]string{missingPath},
		nil,
		true,
		now.Add(10*time.Minute).Unix(),
		now.Add(20*time.Minute).Unix(),
	)

	if !errors.Is(err, upload.ErrTimeWindowNotReady) {
		t.Fatalf("error = %v, want ErrTimeWindowNotReady", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
}

func TestFileScanHandlersReturnRecognizableTimeWindowStatus(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name          string
		handler       func(http.ResponseWriter, *http.Request)
		startTime     int64
		endTime       int64
		wantErrorCode string
	}{
		{
			name:          "invalid window",
			handler:       (&Server{}).handleFileScan,
			startTime:     now.Unix(),
			endTime:       now.Add(-time.Minute).Unix(),
			wantErrorCode: master.TaskErrorCodeInvalidTimeWindow,
		},
		{
			name:          "future window",
			handler:       (&Server{}).handleFileScanByContent,
			startTime:     now.Add(10 * time.Minute).Unix(),
			endTime:       now.Add(20 * time.Minute).Unix(),
			wantErrorCode: master.TaskErrorCodeTimeWindowNotReady,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body bytes.Buffer
			if err := json.NewEncoder(&body).Encode(master.TaskRequest{
				TaskID:      tc.name,
				StartTime:   tc.startTime,
				EndTime:     tc.endTime,
				ScanFolders: []string{filepath.Join(t.TempDir(), "must-not-be-accessed")},
			}); err != nil {
				t.Fatalf("encode request: %v", err)
			}

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", &body)
			tc.handler(recorder, request)

			var response master.TaskResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Success {
				t.Fatalf("response = %+v, want unsuccessful scan", response)
			}
			if response.ErrorCode != tc.wantErrorCode {
				t.Fatalf("error code = %q, want %q", response.ErrorCode, tc.wantErrorCode)
			}
			if response.Error == "" {
				t.Fatalf("response = %+v, want diagnostic error text", response)
			}
		})
	}
}
