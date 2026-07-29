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

func TestDownloadSlaveFileFailsWhenResponseHeadersExceedRequestTimeout(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseResponse
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(releaseResponse)
		server.Close()
	}()

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

	result := make(chan error, 1)
	go func() {
		reader, err := client.DownloadSlaveFile(ctx, slave, "/tmp/source.bag")
		if reader != nil {
			_ = reader.Close()
		}
		result <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("server did not receive download request")
	}

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("DownloadSlaveFile succeeded after response header timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("DownloadSlaveFile did not time out waiting for response headers")
	}
}

func TestDownloadSlaveFileBodyReadStopsWhenCallerContextIsCancelled(t *testing.T) {
	t.Parallel()

	const firstChunk = "first"
	headersFlushed := make(chan struct{})
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(firstChunk)); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		close(headersFlushed)
		<-r.Context().Done()
		close(requestCancelled)
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

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	reader, err := client.DownloadSlaveFile(ctx, slave, "/tmp/source.bag")
	if err != nil {
		t.Fatalf("start download: %v", err)
	}
	defer reader.Close()

	select {
	case <-headersFlushed:
	case <-time.After(time.Second):
		t.Fatal("server did not flush response headers")
	}

	content := make([]byte, len(firstChunk))
	if _, err := io.ReadFull(reader, content); err != nil {
		t.Fatalf("read first response chunk: %v", err)
	}
	if got := string(content); got != firstChunk {
		t.Fatalf("first response chunk = %q, want %q", got, firstChunk)
	}

	cancel()

	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("body read succeeded after caller context cancellation")
	}
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("server did not observe caller context cancellation")
	}
}
