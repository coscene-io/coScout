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

package collector

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/model"
)

func TestDownloadSlaveFileToLocalUsesLatestCompleteContent(t *testing.T) {
	t.Parallel()

	const slaveID = "0011223344556677"

	tests := []struct {
		name          string
		content       string
		contentLength int64
		scannedSize   int64
		wantErr       bool
	}{
		{
			name:        "accepts file truncated after scan",
			content:     "short",
			scannedSize: 10,
			wantErr:     false,
		},
		{
			name:        "accepts file appended after scan",
			content:     "complete-and-appended",
			scannedSize: 8,
			wantErr:     false,
		},
		{
			name:          "rejects truncated HTTP response",
			content:       "short",
			contentLength: 10,
			scannedSize:   10,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/files/download" {
					http.NotFound(w, r)
					return
				}
				if tt.contentLength > 0 {
					w.Header().Set("Content-Length", strconv.FormatInt(tt.contentLength, 10))
				}
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(tt.content)); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			}))
			defer server.Close()

			host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("failed to parse test server address: %v", err)
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				t.Fatalf("failed to parse test server port: %v", err)
			}

			registry := master.NewSlaveRegistry()
			if err := registry.Register(&master.SlaveInfo{ID: slaveID, IP: host, Port: port}); err != nil {
				t.Fatalf("failed to register slave: %v", err)
			}
			fileManager := master.NewFileManager(master.NewClient(config.DefaultMasterConfig()), registry)

			localPath := filepath.Join(t.TempDir(), "cached-slave-file")
			fileInfo := &model.FileInfo{
				Path: "slave://" + slaveID + "/tmp/source.bag",
				Size: tt.scannedSize,
			}

			err = downloadSlaveFileToLocal(t.Context(), fileManager, fileInfo, localPath, 0)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected truncated slave download to fail")
				}
				if _, statErr := os.Stat(localPath); !os.IsNotExist(statErr) {
					t.Fatalf("expected incomplete cache file to be removed, stat err: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected complete slave file download to succeed: %v", err)
			}
			data, err := os.ReadFile(localPath)
			if err != nil {
				t.Fatalf("failed to read cached slave file: %v", err)
			}
			if string(data) != tt.content {
				t.Fatalf("cached content = %q, want %q", string(data), tt.content)
			}
		})
	}
}
