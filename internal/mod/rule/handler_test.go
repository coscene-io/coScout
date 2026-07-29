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
	"errors"
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

var errTestReadFailed = errors.New("read failed")

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

			handler.handleCollectInfo(t.Context(), info, config.DefaultModConfConfig{})

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

func (*recordingRuleFileStateHandler) MarkFailedFile(string) error {
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
	failedFile    []string
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

func (f *fakeFileStateHandler) MarkFailedFile(filename string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedFile = append(f.failedFile, filename)
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

func (f *fakeFileStateHandler) failedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.failedFile)
}

type cancellationBlockingHandler struct {
	started chan struct{}
	err     error
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
) error {
	if h.started != nil {
		select {
		case h.started <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	if h.err != nil {
		return h.err
	}
	return ctx.Err()
}

type resultFileHandler struct {
	err error
}

func (h *resultFileHandler) CheckFilePath(string) bool {
	return true
}

func (h *resultFileHandler) GetStartTimeEndTime(string) (*time.Time, *time.Time, error) {
	return nil, nil, nil
}

func (h *resultFileHandler) GetFileSize(string) (int64, error) {
	return 0, nil
}

func (h *resultFileHandler) IsFinished(string) bool {
	return true
}

func (h *resultFileHandler) SendRuleItems(
	context.Context,
	string,
	mapset.Set[string],
	chan<- rule_engine.RuleItem,
) error {
	return h.err
}

type itemProducingFileHandler struct {
	produced chan struct{}
}

func (h *itemProducingFileHandler) CheckFilePath(string) bool {
	return true
}

func (h *itemProducingFileHandler) GetStartTimeEndTime(string) (*time.Time, *time.Time, error) {
	return nil, nil, nil
}

func (h *itemProducingFileHandler) GetFileSize(string) (int64, error) {
	return 0, nil
}

func (h *itemProducingFileHandler) IsFinished(string) bool {
	return true
}

func (h *itemProducingFileHandler) SendRuleItems(
	ctx context.Context,
	filename string,
	_ mapset.Set[string],
	ruleItems chan<- rule_engine.RuleItem,
) error {
	select {
	case ruleItems <- rule_engine.RuleItem{
		Source: filename,
		Topic:  "/fault",
	}:
		if h.produced != nil {
			close(h.produced)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type multiItemFileHandler struct {
	resultFileHandler
	done chan struct{}
}

func (h *multiItemFileHandler) SendRuleItems(
	ctx context.Context,
	filename string,
	_ mapset.Set[string],
	ruleItems chan<- rule_engine.RuleItem,
) error {
	defer close(h.done)
	for index := 1; index <= 2; index++ {
		select {
		case ruleItems <- rule_engine.RuleItem{
			Msg:    map[string]interface{}{"index": index},
			Source: filename,
			Topic:  "/fault",
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestSendFilesToBeProcessedCancellationDoesNotMarkUnqueuedFile(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "pending.log"}},
	}
	listenChan := make(chan string, 1)
	listenChan <- "already-queued.log"
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             listenChan,
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
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
	require.Zero(t, stateHandler.failedCount())
	require.False(t, handler.isFileInFlight("pending.log"))
}

func TestProcessListenedFilesCancellationInterruptsSemaphoreWait(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		fileHandler: &cancellationBlockingHandler{},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 2),
		ruleItemChan:           make(chan ruleItemEnvelope),
	}
	handler.listenChan <- "first.log"
	handler.listenChan <- "second.log"

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
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

func TestCancelledFileProcessingDoesNotMarkFileProcessed(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	stateHandler := &fakeFileStateHandler{
		files:       []file_state_handler.FileState{{Pathname: "pending.log"}},
		fileHandler: &cancellationBlockingHandler{started: started},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})
	require.Zero(t, stateHandler.processedCount())
	require.True(t, handler.isFileInFlight("pending.log"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

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
	require.Zero(t, stateHandler.failedCount())
	require.False(t, handler.isFileInFlight("pending.log"))
}

func TestCancellationDoesNotMarkFailedWhenHandlerReturnsSentinelError(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	sentinelErr := errors.New("handler lost cancellation cause")
	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "cancelled-sentinel.log"}},
		fileHandler: &cancellationBlockingHandler{
			started: started,
			err:     sentinelErr,
		},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.False(t, shouldMarkFileFailed(ctx, sentinelErr))
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.processedCount())
	require.Zero(t, stateHandler.failedCount())
	require.False(t, handler.isFileInFlight("cancelled-sentinel.log"))
}

func TestCompletedFileProcessingMarksFileProcessed(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		files:       []file_state_handler.FileState{{Pathname: "completed.log"}},
		fileHandler: &resultFileHandler{},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})
	require.Zero(t, stateHandler.processedCount())

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	require.Eventually(t, func() bool {
		return stateHandler.processedCount() == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.failedCount())
	require.False(t, handler.isFileInFlight("completed.log"))

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

func TestFailedFileProcessingDoesNotMarkFileProcessed(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "failed.log"}},
		fileHandler: &resultFileHandler{
			err: errTestReadFailed,
		},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})
	require.True(t, handler.isFileInFlight("failed.log"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	require.Eventually(t, func() bool {
		return stateHandler.failedCount() == 1 &&
			!handler.isFileInFlight("failed.log")
	}, time.Second, 10*time.Millisecond)

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
}

func TestSendFilesToBeProcessedDoesNotEnqueueInFlightFileTwice(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "pending.log"}},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 2),
	}
	ctx := t.Context()
	modConfig := &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	}

	handler.sendFilesToBeProcessed(ctx, modConfig)
	handler.sendFilesToBeProcessed(ctx, modConfig)

	require.Len(t, handler.listenChan, 1)
	require.Zero(t, stateHandler.processedCount())
	require.True(t, handler.isFileInFlight("pending.log"))
}

func TestProducedRuleItemWithoutConsumerAckIsNotMarkedProcessedOnCancel(t *testing.T) {
	t.Parallel()

	produced := make(chan struct{})
	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "pending-ack.log"}},
		fileHandler: &itemProducingFileHandler{
			produced: produced,
		},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope, 1),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-produced:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	var envelope ruleItemEnvelope
	select {
	case envelope = <-handler.ruleItemChan:
	case <-time.After(time.Second):
		t.Fatal("rule item was not forwarded to the consumer channel")
	}
	require.NotNil(t, envelope.result)
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
	require.Zero(t, stateHandler.failedCount())
	select {
	case envelope.result <- nil:
	case <-time.After(time.Second):
		t.Fatal("consumer result handshake blocked after worker cancellation")
	}
}

func TestProducedRuleItemIsMarkedProcessedOnlyAfterConsumerAck(t *testing.T) {
	t.Parallel()

	stateHandler := &fakeFileStateHandler{
		files:       []file_state_handler.FileState{{Pathname: "acked.log"}},
		fileHandler: &itemProducingFileHandler{},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope, 1),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()

	var envelope ruleItemEnvelope
	select {
	case envelope = <-handler.ruleItemChan:
	case <-time.After(time.Second):
		t.Fatal("rule item was not forwarded to the consumer channel")
	}
	require.NotNil(t, envelope.result)
	require.Zero(t, stateHandler.processedCount())

	envelope.result <- nil
	require.Eventually(t, func() bool {
		return stateHandler.processedCount() == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.failedCount())

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

func TestRuleItemConsumptionErrorMarksFileFailed(t *testing.T) {
	t.Parallel()

	consumeErr := errors.New("consume failed")
	action, err := rule_engine.NewAction(
		"failing-action",
		map[string]interface{}{},
		func(map[string]interface{}) error {
			return consumeErr
		},
	)
	require.NoError(t, err)
	stateHandler := &fakeFileStateHandler{
		files:       []file_state_handler.FileState{{Pathname: "consume-error.log"}},
		fileHandler: &itemProducingFileHandler{},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope, 1),
		engine: Engine{
			rules:            []*rule_engine.Rule{testRuntimeRule(t, action)},
			ruleDebounceTime: make(map[string]*time.Time),
			publishCollectInfoFn: func(string) error {
				return nil
			},
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		select {
		case envelope := <-handler.ruleItemChan:
			envelope.result <- handler.engine.ConsumeNext(envelope.item)
		case <-ctx.Done():
		}
	}()

	require.Eventually(t, func() bool {
		return stateHandler.failedCount() == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.processedCount())
	require.Eventually(t, func() bool {
		select {
		case <-consumerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

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

func TestRuleItemConsumptionErrorDrainsRemainingHandlerItems(t *testing.T) {
	t.Parallel()

	producerDone := make(chan struct{})
	stateHandler := &fakeFileStateHandler{
		files: []file_state_handler.FileState{{Pathname: "multi-item.log"}},
		fileHandler: &multiItemFileHandler{
			done: producerDone,
		},
	}
	handler := &CustomRuleHandler{
		listenFileStateHandler: stateHandler,
		listenChan:             make(chan string, 1),
		ruleItemChan:           make(chan ruleItemEnvelope, 1),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler.sendFilesToBeProcessed(ctx, &config.DefaultModConfConfig{
		ListenDirs: []string{t.TempDir()},
	})

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		handler.processListenedFilesAndSendMessages(ctx, 1)
	}()

	var consumedIndexes []int
	consumerDone := make(chan struct{})
	firstConsumeErr := errors.New("first item failed")
	go func() {
		defer close(consumerDone)
		for index := 0; index < 2; index++ {
			select {
			case envelope := <-handler.ruleItemChan:
				itemIndex, _ := envelope.item.Msg["index"].(int)
				consumedIndexes = append(consumedIndexes, itemIndex)
				if index == 0 {
					envelope.result <- firstConsumeErr
				} else {
					envelope.result <- nil
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	require.Eventually(t, func() bool {
		select {
		case <-producerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-consumerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, []int{1, 2}, consumedIndexes)
	require.Eventually(t, func() bool {
		return stateHandler.failedCount() == 1
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, stateHandler.processedCount())

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-workerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}
