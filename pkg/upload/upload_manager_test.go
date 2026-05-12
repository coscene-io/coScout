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

package upload

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestUploadManagerCloseStopsProgressHandler(t *testing.T) {
	t.Parallel()

	manager, err := NewUploadManager(nil, nil, "", make(chan *model.NetworkUsage, 1))
	if err != nil {
		t.Fatalf("new upload manager: %v", err)
	}

	manager.uploadProgressChan <- FileUploadProgress{
		SessionID: "session-1",
		Name:      "/tmp/file.bag",
		Uploaded:  1,
		TotalSize: 1,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Close()
		manager.Close()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("upload manager close did not stop progress handler")
	}
}

func TestUploadManagerCloseDoesNotBlockWhenNetworkChannelIsFull(t *testing.T) {
	t.Parallel()

	manager, err := NewUploadManager(nil, nil, "", make(chan *model.NetworkUsage))
	if err != nil {
		t.Fatalf("new upload manager: %v", err)
	}

	manager.uploadProgressChan <- FileUploadProgress{
		SessionID: "session-1",
		Name:      "/tmp/file.bag",
		Uploaded:  1,
		TotalSize: 2,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Close()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("upload manager close blocked on network usage channel")
	}
}

func TestFPutObjectSinglePutUsesFixedUploadSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "growing-file.log")
	if err := os.WriteFile(filePath, []byte("snapshot-appended"), 0600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	transport := &capturePutObjectTransport{}
	client, err := minio.New("127.0.0.1:9000", &minio.Options{
		Creds:     credentials.NewStaticV4("access-key", "secret-key", ""),
		Secure:    false,
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("new minio client: %v", err)
	}

	manager, err := NewUploadManager(client, nil, "", make(chan *model.NetworkUsage, 10))
	if err != nil {
		t.Fatalf("new upload manager: %v", err)
	}
	defer manager.Close()

	const fixedSize = int64(len("snapshot"))
	if err := manager.FPutObject(t.Context(), "session-1", filePath, "bucket", "object.log", fixedSize, nil, false); err != nil {
		t.Fatalf("put object: %v, requests: %v, body: %q", err, transport.requests, transport.body.String())
	}

	gotBody := transport.body.String()
	if !strings.Contains(gotBody, "snapshot") {
		t.Fatalf("uploaded body %q does not contain expected snapshot bytes", gotBody)
	}
	if strings.Contains(gotBody, "appended") {
		t.Fatalf("uploaded body %q contains bytes beyond fixed upload size", gotBody)
	}
	if transport.decodedContentLength != fixedSize {
		t.Fatalf("decoded content length = %d, want %d", transport.decodedContentLength, fixedSize)
	}
}

type capturePutObjectTransport struct {
	mu                   sync.Mutex
	body                 bytes.Buffer
	contentLength        int64
	decodedContentLength int64
	requests             []string
}

func (t *capturePutObjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.Method+" "+req.URL.Path+"?"+req.URL.RawQuery)
	t.mu.Unlock()

	if req.Method == http.MethodGet && req.URL.Query().Get("location") == "" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("<LocationConstraint></LocationConstraint>")),
			Request:    req,
		}, nil
	}

	if req.Method == http.MethodPut && strings.Contains(req.URL.Path, "/object.log") {
		t.mu.Lock()
		defer t.mu.Unlock()

		t.contentLength = req.ContentLength
		if decoded := req.Header.Get("X-Amz-Decoded-Content-Length"); decoded != "" {
			decodedContentLength, err := strconv.ParseInt(decoded, 10, 64)
			if err != nil {
				return nil, err
			}
			t.decodedContentLength = decodedContentLength
		}
		if _, err := io.Copy(&t.body, req.Body); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Etag": []string{`"test-etag"`},
		},
		Body:    io.NopCloser(strings.NewReader("")),
		Request: req,
	}, nil
}
