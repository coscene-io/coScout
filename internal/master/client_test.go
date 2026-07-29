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

package master

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
)

func TestDownloadSlaveFileAllowsBodyTransferBeyondRequestTimeout(t *testing.T) {
	t.Parallel()

	const (
		firstChunk  = "first"
		secondChunk = "second"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/download" {
			http.NotFound(w, r)
			return
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(firstChunk)); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		time.Sleep(75 * time.Millisecond)
		_, _ = w.Write([]byte(secondChunk))
	}))
	defer server.Close()

	host, portRaw, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	masterConfig := config.DefaultMasterConfig()
	masterConfig.RequestTimeout = 25 * time.Millisecond
	client := NewClient(masterConfig)
	slave := &SlaveInfo{ID: "0011223344556677", IP: host, Port: port}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	reader, err := client.DownloadSlaveFile(ctx, slave, "/tmp/source.bag")
	if err != nil {
		t.Fatalf("start download: %v", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if got, want := string(content), firstChunk+secondChunk; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}
