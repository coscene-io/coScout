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

func TestDownloadSlaveFileToLocalRejectsShortRead(t *testing.T) {
	t.Parallel()

	const slaveID = "0011223344556677"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/download" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("short")); err != nil {
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
		Size: 10,
	}

	err = downloadSlaveFileToLocal(t.Context(), fileManager, fileInfo, localPath, 0)
	if err == nil {
		t.Fatal("expected short slave download to fail")
	}
	if _, statErr := os.Stat(localPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected incomplete cache file to be removed, stat err: %v", statErr)
	}
}
