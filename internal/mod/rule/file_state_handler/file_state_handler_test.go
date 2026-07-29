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

package file_state_handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod/rule/file_handlers"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/require"
)

type blockingFinishedHandler struct {
	started chan struct{}
	release chan struct{}
}

type fileInfoWithModTime struct {
	os.FileInfo
	modTime time.Time
}

func (i fileInfoWithModTime) ModTime() time.Time {
	return i.modTime
}

func (h *blockingFinishedHandler) CheckFilePath(string) bool {
	return true
}

func (h *blockingFinishedHandler) GetStartTimeEndTime(string) (*time.Time, *time.Time, error) {
	now := time.Now()
	return &now, &now, nil
}

func (h *blockingFinishedHandler) GetFileSize(string) (int64, error) {
	return 0, nil
}

func (h *blockingFinishedHandler) IsFinished(string) bool {
	if h.started != nil {
		close(h.started)
	}
	if h.release != nil {
		<-h.release
	}
	return true
}

func (h *blockingFinishedHandler) SendRuleItems(
	context.Context,
	string,
	mapset.Set[string],
	func(rule_engine.RuleItem) bool,
) error {
	return nil
}

func newTestFileStateHandler(t *testing.T) *fileStateHandler {
	t.Helper()

	return &fileStateHandler{
		state:        make(map[string]FileState),
		listenDirs:   mapset.NewSet[string](),
		collectDirs:  mapset.NewSet[string](),
		activeTopics: mapset.NewSet[string](),
		statePath:    filepath.Join(t.TempDir(), "file-state.json"),
	}
}

func TestRecursiveDirectoryUpdatesRollBackAfterLaterTraversalError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update func(*fileStateHandler, config.DefaultModConfConfig) error
	}{
		{
			name: "listen dirs",
			update: func(handler *fileStateHandler, conf config.DefaultModConfConfig) error {
				return handler.UpdateListenDirs(conf)
			},
		},
		{
			name: "collect dirs",
			update: func(handler *fileStateHandler, conf config.DefaultModConfConfig) error {
				return handler.UpdateCollectDirs(nil, conf)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			firstPath := filepath.Join(root, "a-file.txt")
			if err := os.WriteFile(firstPath, []byte("data"), 0o600); err != nil {
				t.Fatalf("write first file: %v", err)
			}
			if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "z-broken-link")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			original := FileState{Size: 123, ModifyTime: 456, Unsupported: false}
			handler := &fileStateHandler{
				state:        map[string]FileState{firstPath: original},
				listenDirs:   mapset.NewSet[string](),
				collectDirs:  mapset.NewSet[string](),
				activeTopics: mapset.NewSet[string](),
				statePath:    filepath.Join(t.TempDir(), "state.json"),
			}
			conf := config.DefaultModConfConfig{
				ListenDirs:          []string{root},
				CollectDirs:         []string{root},
				RecursivelyWalkDirs: true,
			}

			if err := tc.update(handler, conf); err != nil {
				t.Fatalf("directory update error = %v", err)
			}
			if !reflect.DeepEqual(handler.state, map[string]FileState{firstPath: original}) {
				t.Fatalf("state = %#v, want original state restored after later traversal error", handler.state)
			}
		})
	}
}

func TestConcurrentStateAccessAndPersistence(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	baseDir := t.TempDir()
	const iterations = 500

	var wg sync.WaitGroup
	errs := make(chan error, iterations)

	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := range iterations {
			filename := filepath.Join(baseDir, fmt.Sprintf("%d.log", i))
			handler.setFileState(filename, FileState{Size: int64(i), IsListening: true})
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			_ = handler.Files()
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			if err := handler.saveState(); err != nil {
				errs <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range iterations {
			_, _ = handler.getFileState(filepath.Join(baseDir, fmt.Sprintf("%d.log", i)))
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestConcurrentDirectoryUpdates(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	listenDir := t.TempDir()
	collectDir := t.TempDir()
	const iterations = 25

	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		for range iterations {
			if err := handler.UpdateListenDirs(config.DefaultModConfConfig{
				ListenDirs: []string{listenDir},
			}); err != nil {
				errs <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			if err := handler.UpdateCollectDirs(nil, config.DefaultModConfConfig{
				CollectDirs: []string{collectDir},
			}); err != nil {
				errs <- err
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestProcessListenFileDoesNotRevertConcurrentProcessedState(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	filePath := filepath.Join(t.TempDir(), "active.log")
	require.NoError(t, os.WriteFile(filePath, []byte("log"), 0o600))
	info, err := os.Stat(filePath)
	require.NoError(t, err)

	handler.setFileState(filePath, FileState{
		Size:         info.Size(),
		ModifyTime:   info.ModTime().UnixNano(),
		IsListening:  true,
		ProcessState: processStateSeenOnce,
	})
	blockingHandler := &blockingFinishedHandler{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler.handlers = []file_handlers.Interface{blockingHandler}

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.processListenFile(filePath, info, 24)
	}()

	<-blockingHandler.started
	require.NoError(t, handler.MarkProcessedFile(filePath))
	close(blockingHandler.release)
	<-done

	state, exists := handler.getFileState(filePath)
	require.True(t, exists)
	require.Equal(t, processStateProcessed, state.ProcessState)
}

func TestFailedFileIsNotReadyUntilItsContentsChange(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	filePath := filepath.Join(t.TempDir(), "failed.log")
	require.NoError(t, os.WriteFile(filePath, []byte("old"), 0o600))
	info, err := os.Stat(filePath)
	require.NoError(t, err)

	handler.setFileState(filePath, FileState{
		Size:         info.Size(),
		ModifyTime:   info.ModTime().UnixNano(),
		IsListening:  true,
		ProcessState: processStateReadyToProcess,
	})
	require.NoError(t, handler.MarkFailedFile(filePath))
	require.NoError(t, handler.UpdateFilesProcessState())
	require.NoError(t, handler.UpdateFilesProcessState())
	require.Empty(t, handler.Files(FilterReadyToProcess()))

	require.NoError(t, os.WriteFile(filePath, []byte("new contents"), 0o600))
	modifiedInfo, err := os.Stat(filePath)
	require.NoError(t, err)
	handler.handlers = []file_handlers.Interface{&blockingFinishedHandler{}}

	handler.processListenFile(filePath, modifiedInfo, 24)

	state, exists := handler.getFileState(filePath)
	require.True(t, exists)
	require.Equal(t, processStateUnprocessed, state.ProcessState)
	require.Equal(t, modifiedInfo.Size(), state.Size)
}

func TestFailedFileRetriesWhenOnlyModTimeNanosecondsChange(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	filePath := filepath.Join(t.TempDir(), "same-size.log")
	require.NoError(t, os.WriteFile(filePath, []byte("same"), 0o600))
	info, err := os.Stat(filePath)
	require.NoError(t, err)

	oldModTime := time.Now().Truncate(time.Second).Add(100 * time.Nanosecond)
	newModTime := oldModTime.Add(time.Nanosecond)
	handler.setFileState(filePath, FileState{
		Size:         info.Size(),
		ModifyTime:   oldModTime.UnixNano(),
		IsListening:  true,
		ProcessState: processStateFailed,
	})
	handler.handlers = []file_handlers.Interface{&blockingFinishedHandler{}}

	handler.processListenFile(filePath, fileInfoWithModTime{
		FileInfo: info,
		modTime:  newModTime,
	}, 24)

	state, exists := handler.getFileState(filePath)
	require.True(t, exists)
	require.Equal(t, processStateUnprocessed, state.ProcessState)
	require.Equal(t, newModTime.UnixNano(), state.ModifyTime)
}

func TestRestoreFileStatesPreservesConcurrentProcessState(t *testing.T) {
	t.Parallel()

	handler := newTestFileStateHandler(t)
	filePath := filepath.Join(t.TempDir(), "restore.log")
	original := FileState{
		Size:         10,
		ModifyTime:   20,
		IsListening:  true,
		ProcessState: processStateSeenOnce,
	}
	handler.setFileState(filePath, original)

	checkpoints := make(map[string]fileStateCheckpoint)
	handler.checkpointFileState(checkpoints, filePath)
	require.NoError(t, handler.MarkProcessedFile(filePath))
	handler.restoreFileStates(checkpoints)

	state, exists := handler.getFileState(filePath)
	require.True(t, exists)
	require.Equal(t, processStateProcessed, state.ProcessState)
	require.Equal(t, original.Size, state.Size)
	require.Equal(t, original.ModifyTime, state.ModifyTime)
}
