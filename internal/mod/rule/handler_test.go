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

package rule

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod/rule/file_handlers"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/require"
)

func TestHandleCollectInfoTimeWindowLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name          string
		start         time.Time
		end           time.Time
		responses     map[string]*master.TaskResponse
		wantClean     bool
		wantSlaveCall bool
		wantStateScan bool
	}{
		{
			name:      "future local window is consumed as empty success",
			start:     now.Add(10 * time.Minute),
			end:       now.Add(20 * time.Minute),
			wantClean: true,
		},
		{
			name:      "invalid local window is cleaned",
			start:     now,
			end:       now.Add(-time.Minute),
			wantClean: true,
		},
		{
			name:  "invalid slave window wins and is cleaned",
			start: now.Add(-time.Minute),
			end:   now.Add(time.Minute),
			responses: map[string]*master.TaskResponse{
				"future": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
				},
				"invalid": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeInvalidTimeWindow,
				},
			},
			wantClean:     true,
			wantSlaveCall: true,
			wantStateScan: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cleaned := false
			requester := &recordingRuleSlaveFileRequester{responses: tc.responses}
			fileStates := &recordingRuleFileStateHandler{}
			handler := &CustomRuleHandler{
				collectFileStateHandler: fileStates,
				slaveRegistry:           master.NewSlaveRegistry(),
				masterClient:            requester,
				masterConfig:            &config.MasterConfig{RequestTimeout: time.Second},
				cleanCollectInfo: func(model.CollectInfo) string {
					cleaned = true
					return "cleaned"
				},
			}
			info := model.CollectInfo{
				Id: "collect-info",
				Cut: &model.CollectInfoCut{
					Start: tc.start.Unix(),
					End:   tc.end.Unix(),
				},
			}

			handler.handleCollectInfo(info, config.DefaultModConfConfig{})

			if cleaned != tc.wantClean {
				t.Fatalf("cleaned = %v, want %v", cleaned, tc.wantClean)
			}
			if requester.called != tc.wantSlaveCall {
				t.Fatalf("slave called = %v, want %v", requester.called, tc.wantSlaveCall)
			}
			if got := fileStates.updateCalls > 0; got != tc.wantStateScan {
				t.Fatalf("file state scan = %v, want %v", got, tc.wantStateScan)
			}
		})
	}
}

func TestSuccessfulSlaveResponsesPreservesResultsAlongsideLegacyNotReady(t *testing.T) {
	t.Parallel()

	success := &master.TaskResponse{
		Success: true,
		Files: []master.SlaveFileInfo{
			{
				FileInfo: model.FileInfo{Path: "/var/log/healthy.log"},
				SlaveID:  "healthy",
			},
		},
	}
	responses := map[string]*master.TaskResponse{
		"legacy": {
			Success:   false,
			ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
		},
		"healthy": success,
		"missing": nil,
	}

	got := successfulSlaveResponses(responses)
	if len(got) != 1 {
		t.Fatalf("successful response count = %d, want 1", len(got))
	}
	if got["healthy"] != success {
		t.Fatalf("healthy response = %#v, want %#v", got["healthy"], success)
	}
	if _, ok := got["legacy"]; ok {
		t.Fatal("legacy not-ready response was treated as successful")
	}
}

type recordingRuleSlaveFileRequester struct {
	responses map[string]*master.TaskResponse
	called    bool
}

func (r *recordingRuleSlaveFileRequester) RequestAllSlaveFilesByContent(
	context.Context,
	*master.SlaveRegistry,
	*master.TaskRequest,
) map[string]*master.TaskResponse {
	r.called = true
	return r.responses
}

type recordingRuleFileStateHandler struct {
	updateCalls int
}

func (*recordingRuleFileStateHandler) UpdateListenDirs(config.DefaultModConfConfig) error {
	return nil
}

func (r *recordingRuleFileStateHandler) UpdateCollectDirs([]string, config.DefaultModConfConfig) error {
	r.updateCalls++
	return nil
}

func (*recordingRuleFileStateHandler) Files(...file_state_handler.FileFilter) []file_state_handler.FileState {
	return nil
}

func (*recordingRuleFileStateHandler) UpdateFilesProcessState() error {
	return nil
}

func (*recordingRuleFileStateHandler) MarkProcessedFile(string) error {
	return nil
}

func (*recordingRuleFileStateHandler) GetFileHandler(string) file_handlers.Interface {
	return nil
}

type fakeFileStateHandler struct {
	files       []file_state_handler.FileState
	fileHandler file_handlers.Interface

	mu            sync.Mutex
	processedFile []string
}

func (f *fakeFileStateHandler) UpdateListenDirs(config.DefaultModConfConfig) error {
	return nil
}

func (f *fakeFileStateHandler) UpdateCollectDirs([]string, config.DefaultModConfConfig) error {
	return nil
}

func (f *fakeFileStateHandler) Files(...file_state_handler.FileFilter) []file_state_handler.FileState {
	return f.files
}

func (f *fakeFileStateHandler) UpdateFilesProcessState() error {
	return nil
}

func (f *fakeFileStateHandler) MarkProcessedFile(filename string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processedFile = append(f.processedFile, filename)
	return nil
}

func (f *fakeFileStateHandler) GetFileHandler(string) file_handlers.Interface {
	return f.fileHandler
}

func (f *fakeFileStateHandler) processedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.processedFile)
}

type cancellationBlockingHandler struct {
}

func (h *cancellationBlockingHandler) CheckFilePath(string) bool {
	return true
}

func (h *cancellationBlockingHandler) GetStartTimeEndTime(string) (*time.Time, *time.Time, error) {
	return nil, nil, nil
}

func (h *cancellationBlockingHandler) GetFileSize(string) (int64, error) {
	return 0, nil
}

func (h *cancellationBlockingHandler) IsFinished(string) bool {
	return true
}

func (h *cancellationBlockingHandler) SendRuleItems(
	ctx context.Context,
	_ string,
	_ mapset.Set[string],
	_ chan<- rule_engine.RuleItem,
) {
	<-ctx.Done()
}

func TestSendFilesToBeProcessedCancellationDoesNotMarkUnqueuedFile(t *testing.T) {
	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "pending.log"}},
	}
	listenChan := make(chan string, 1)
	listenChan <- "already-queued.log"
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             listenChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	listenDir := t.TempDir()

	go func() {
		defer close(done)
		handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
			ListenDirs: []string{listenDir},
		})
	}()

	select {
	case <-done:
		t.Fatal("sendFilesToBeProcessed returned before the full channel was cancelled")
	case <-time.After(50 * time.Millisecond):
	}
	require.Zero(t, stateHandler.processedCount())

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.processedCount())
}

func TestProcessListenedFilesCancellationInterruptsSemaphoreWait(t *testing.T) {
	stateHandler := &fakeFileStateHandler{
		fileHandler: &cancellationBlockingHandler{},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 2),
		ruleItemChan:           make(chan rule_engine.RuleItem),
	}
	handler.listenChan <- "first.log"
	handler.listenChan <- "second.log"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}
