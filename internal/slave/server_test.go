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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod/rule/file_handlers"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/pkg/upload"
)

type updateCollectDirsErrorHandler struct{}

func (updateCollectDirsErrorHandler) UpdateListenDirs(config.DefaultModConfConfig) error { return nil }

func (updateCollectDirsErrorHandler) UpdateCollectDirs([]string, config.DefaultModConfConfig) error {
	return errors.New("update collect directories failed")
}

func (updateCollectDirsErrorHandler) Files(...file_state_handler.FileFilter) []file_state_handler.FileState {
	return nil
}

func (updateCollectDirsErrorHandler) UpdateFilesProcessState() error { return nil }

func (updateCollectDirsErrorHandler) MarkProcessedFile(string) error { return nil }

func (updateCollectDirsErrorHandler) GetFileHandler(string) file_handlers.Interface { return nil }

type deadlineResponseWriter struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	deadline time.Time
}

func (w *deadlineResponseWriter) Header() http.Header { return w.header }

func (w *deadlineResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *deadlineResponseWriter) WriteHeader(status int) { w.status = status }

func (w *deadlineResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

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

func TestScanFilesReturnsEmptySuccessForFutureWindowBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	now := time.Now()
	files, err := (&Server{}).scanFiles(
		"future",
		[]string{missingPath},
		[]string{missingPath},
		now.Add(10*time.Minute).Unix(),
		now.Add(20*time.Minute).Unix(),
	)

	if err != nil {
		t.Fatalf("error = %v, want empty success", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
}

func TestScanFilesByContentReturnsEmptySuccessForFutureWindowBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	now := time.Now()
	server := &Server{
		hasBirthTime: func([]string) bool {
			t.Fatal("birth-time probe was called for a future window")
			return false
		},
	}
	files, err := server.scanFilesByContent(
		"future",
		[]string{missingPath},
		[]string{missingPath},
		nil,
		true,
		now.Add(10*time.Minute).Unix(),
		now.Add(20*time.Minute).Unix(),
	)

	if err != nil {
		t.Fatalf("error = %v, want empty success", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
}

func TestScanFilesByContentRejectsInvalidWindowBeforeFilesystemAccess(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "must-not-be-accessed")
	server := &Server{
		hasBirthTime: func([]string) bool {
			t.Fatal("birth-time probe was called for an invalid window")
			return false
		},
	}
	files, err := server.scanFilesByContent(
		"invalid",
		[]string{missingPath},
		[]string{missingPath},
		nil,
		true,
		2,
		1,
	)

	if !errors.Is(err, upload.ErrInvalidTimeWindow) {
		t.Fatalf("error = %v, want ErrInvalidTimeWindow", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want none", files)
	}
}

func TestFileScanByContentReportsUnavailableStateHandler(t *testing.T) {
	t.Parallel()

	now := time.Now()
	server := &Server{
		hasBirthTime: func([]string) bool {
			return false
		},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(master.TaskRequest{
		TaskID:    "missing-state-handler",
		StartTime: now.Add(-time.Minute).Unix(),
		EndTime:   now.Unix(),
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	server.handleFileScanByContent(recorder, request)

	var response master.TaskResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Success {
		t.Fatalf("response = %+v, want an explicit failure", response)
	}
	if response.ErrorCode != master.TaskErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", response.ErrorCode, master.TaskErrorCodeInternal)
	}
	if response.Error == "" {
		t.Fatalf("response = %+v, want diagnostic error text", response)
	}
}

func TestFileScanByContentReportsCollectDirectoryUpdateFailure(t *testing.T) {
	t.Parallel()

	now := time.Now()
	server := &Server{
		hasBirthTime:            func([]string) bool { return false },
		collectFileStateHandler: updateCollectDirsErrorHandler{},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(master.TaskRequest{
		TaskID:    "collect-dir-update-failure",
		StartTime: now.Add(-time.Minute).Unix(),
		EndTime:   now.Unix(),
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.handleFileScanByContent(recorder, httptest.NewRequest(http.MethodPost, "/", &body))

	var response master.TaskResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Success {
		t.Fatalf("response = %+v, want an explicit failure", response)
	}
	if response.ErrorCode != master.TaskErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", response.ErrorCode, master.TaskErrorCodeInternal)
	}
	if response.Error == "" {
		t.Fatalf("response = %+v, want diagnostic error text", response)
	}
}

func TestFileDownloadExtendsWriteDeadlineAndStreamsFile(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "download.txt")
	const contents = "download payload"
	if err := os.WriteFile(filePath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	var requestBody bytes.Buffer
	if err := json.NewEncoder(&requestBody).Encode(master.FileTransferRequest{FilePath: filePath}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	writer := &deadlineResponseWriter{header: make(http.Header)}
	startedAt := time.Now()
	(&Server{}).handleFileDownload(writer, httptest.NewRequest(http.MethodPost, "/", &requestBody))

	if writer.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", writer.status, http.StatusOK)
	}
	if writer.body.String() != contents {
		t.Fatalf("body = %q, want %q", writer.body.String(), contents)
	}
	minDeadline := startedAt.Add(config.DefaultFileTransferTimeout - 5*time.Second)
	maxDeadline := time.Now().Add(config.DefaultFileTransferTimeout + 5*time.Second)
	if writer.deadline.Before(minDeadline) || writer.deadline.After(maxDeadline) {
		t.Fatalf("write deadline = %v, want approximately %v from now", writer.deadline, config.DefaultFileTransferTimeout)
	}
}

func TestFileScanHandlersReturnTimeWindowOutcomes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name          string
		handler       func(http.ResponseWriter, *http.Request)
		startTime     int64
		endTime       int64
		wantSuccess   bool
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
			name:          "invalid window by content time",
			handler:       (&Server{}).handleFileScanByContent,
			startTime:     now.Unix(),
			endTime:       now.Add(-time.Minute).Unix(),
			wantErrorCode: master.TaskErrorCodeInvalidTimeWindow,
		},
		{
			name:        "future window by modification time",
			handler:     (&Server{}).handleFileScan,
			startTime:   now.Add(10 * time.Minute).Unix(),
			endTime:     now.Add(20 * time.Minute).Unix(),
			wantSuccess: true,
		},
		{
			name:        "future window by content time",
			handler:     (&Server{}).handleFileScanByContent,
			startTime:   now.Add(10 * time.Minute).Unix(),
			endTime:     now.Add(20 * time.Minute).Unix(),
			wantSuccess: true,
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
			if response.Success != tc.wantSuccess {
				t.Fatalf("success = %v, want %v: %+v", response.Success, tc.wantSuccess, response)
			}
			if response.ErrorCode != tc.wantErrorCode {
				t.Fatalf("error code = %q, want %q", response.ErrorCode, tc.wantErrorCode)
			}
			if tc.wantSuccess && (response.Error != "" || len(response.Files) != 0) {
				t.Fatalf("response = %+v, want successful empty result", response)
			}
			if !tc.wantSuccess && response.Error == "" {
				t.Fatalf("response = %+v, want diagnostic error text", response)
			}
		})
	}
}
