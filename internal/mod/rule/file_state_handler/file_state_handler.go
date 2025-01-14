// Copyright 2024 coScene
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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler/handlers"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

const (
	// relative file state handler path
	relFileStatePath = "file.state.json"
)

// FileStateHandler is the interface for the file state handler
type FileStateHandler interface {
	// UpdateDirs updates the directories being monitored
	UpdateDirs(conf config.DefaultModConfConfig) error

	// Files returns files matching the given filters
	Files(filters ...FileFilter) []string

	// UpdateFilesProcessState updates the state of files that are ready to process
	// This should be called before processing files
	UpdateFilesProcessState() error

	// MarkProcessedFile updates the state of files that were ready to process
	// to mark them as processed
	MarkProcessedFile(filename string) error
}

// processState represents the state of a file in the processing pipeline
type processState int

const (
	processStateUnprocessed processState = iota
	processStateSeenOnce
	processStateReadyToProcess
	processStateProcessed
)

// FileState represents the state of a file in the system
type FileState struct {
	Size         int64        `json:"size"`
	StartTime    int64        `json:"start_time,omitempty"`
	EndTime      int64        `json:"end_time,omitempty"`
	Unsupported  bool         `json:"unsupported,omitempty"`
	IsDir        bool         `json:"is_dir,omitempty"`
	IsListening  bool         `json:"is_listening,omitempty"`
	IsCollecting bool         `json:"is_collecting,omitempty"`
	ProcessState processState `json:"process_state,omitempty"`
	TooOld       bool         `json:"too_old,omitempty"`
}

// SavedState represents the complete state to be saved to disk
type SavedState struct {
	State      map[string]FileState `json:"state"`
	SrcDirs    []string             `json:"src_dirs"`
	ListenDirs []string             `json:"listen_dirs"`
}

// fileStateHandler is used to keep track of the state of files in directories
type fileStateHandler struct {
	state        map[string]FileState
	updateLock   sync.Mutex
	srcDirs      mapset.Set[string]
	listenDirs   mapset.Set[string]
	activeTopics mapset.Set[string]
	statePath    string
	handlers     []handlers.Interface

	instance *fileStateHandler
}

var (
	instance *fileStateHandler
	once     sync.Once
)

// New returns the singleton instance of fileStateHandler
func New() (FileStateHandler, error) {
	var err error
	once.Do(func() {
		instance = &fileStateHandler{
			state:        make(map[string]FileState),
			srcDirs:      mapset.NewSet[string](),
			listenDirs:   mapset.NewSet[string](),
			activeTopics: mapset.NewSet[string](),
			statePath:    path.Join(config.GetUserBaseFolder(), relFileStatePath),
		}

		instance.registerHandlers()

		if loadErr := instance.loadState(); loadErr != nil {
			err = fmt.Errorf("failed to initialize file state handler: %w", loadErr)
			instance = nil
		}
	})

	if instance == nil {
		if err == nil {
			err = fmt.Errorf("failed to create file state handler instance")
		}
		return nil, err
	}

	return instance, nil
}

// registerHandlers registers the handlers for different file types
func (f *fileStateHandler) registerHandlers() {
	// Register handlers here
	f.handlers = []handlers.Interface{
		handlers.NewLogHandler(),
		handlers.NewMcapHandler(),
		handlers.NewRos1Handler(),
	}
}

// getFileHandler returns the handler for a given file path
func (f *fileStateHandler) getFileHandler(filePath string) handlers.Interface {
	for _, handler := range f.handlers {
		if handler.CheckFilePath(filePath) {
			return handler
		}
	}
	return nil
}

// LoadState loads the file state from disk
func (f *fileStateHandler) loadState() error {
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var savedState SavedState
	if err := json.Unmarshal(data, &savedState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	f.state = savedState.State
	f.srcDirs = mapset.NewSet[string]()
	f.listenDirs = mapset.NewSet[string]()

	for _, dir := range savedState.SrcDirs {
		f.srcDirs.Add(dir)
	}
	for _, dir := range savedState.ListenDirs {
		f.listenDirs.Add(dir)
	}

	return nil
}

// SaveState saves the current state to disk
func (f *fileStateHandler) saveState() error {
	savedState := SavedState{
		State:      f.state,
		SrcDirs:    f.srcDirs.ToSlice(),
		ListenDirs: f.listenDirs.ToSlice(),
	}

	data, err := json.MarshalIndent(savedState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(f.statePath), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	if err := os.WriteFile(f.statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// setFileState updates the state for a given file path
func (f *fileStateHandler) setFileState(filePath string, state FileState) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Errorf("Failed to get absolute path for %s: %v", filePath, err)
		return
	}

	f.updateLock.Lock()
	defer f.updateLock.Unlock()
	f.state[absPath] = state
}

// delFileState removes the state for a given file path
func (f *fileStateHandler) delFileState(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
	}

	f.updateLock.Lock()
	defer f.updateLock.Unlock()
	delete(f.state, absPath)
	return nil
}

// getFileState retrieves the state for a given file path
func (f *fileStateHandler) getFileState(filePath string) (FileState, bool) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Errorf("Failed to get absolute path for %s: %v", filePath, err)
		return FileState{}, false
	}

	state, exists := f.state[absPath]
	return state, exists
}

// updateDeletedFileState removes state entries for files that no longer exist
func (f *fileStateHandler) updateDeletedFileState() error {
	for _, filename := range lo.Keys(f.state) {
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			if err = f.delFileState(filename); err != nil {
				return fmt.Errorf("failed to delete file state for %s: %w", filename, err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to stat file %s: %w", filename, err)
		}
	}
	return nil
}

func (f *fileStateHandler) UpdateDirs(conf config.DefaultModConfConfig) error {
	// Filter directories for read access and create sets
	newListenDirs := mapset.NewSet[string]()
	for _, dir := range conf.ListenDirs {
		if utils.CheckReadPath(dir) {
			newListenDirs.Add(dir)
		}
	}

	newCollectDirs := mapset.NewSet[string]()
	for _, dir := range conf.CollectDirs {
		if utils.CheckReadPath(dir) {
			newCollectDirs.Add(dir)
		}
	}

	log.Infof("Start updating directories, listen_dirs: %v, collect_dirs: %v", newListenDirs, newCollectDirs)

	// Find directories that are no longer being monitored
	deleteDirs := f.srcDirs.Difference(newListenDirs.Union(newCollectDirs))

	// Delete state of files in directories that are no longer scanned or collected
	for filename := range f.state {
		if deleteDirs.Contains(filepath.Dir(filename)) {
			delete(f.state, filename)
		}
	}

	// Iterate over new directories and update file states
	for dir := range newListenDirs.Union(newCollectDirs).Iter() {
		if fileInfo, err := os.Stat(dir); err != nil || !fileInfo.IsDir() {
			log.Warningf("%s is not a directory, skipping", dir)
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Errorf("Failed to read directory %s, skipping: %v", dir, err)
			continue
		}

		for _, entry := range entries {
			entryPath := filepath.Join(dir, entry.Name())
			fileState, hasFileState := f.getFileState(entryPath)

			// Skip unsupported files unless they were marked as too old
			if hasFileState && fileState.Unsupported && !fileState.TooOld {
				continue
			}

			handler := f.getFileHandler(entryPath)
			if handler == nil {
				// No handler supported for file, mark as unsupported if not already
				if !hasFileState || !fileState.Unsupported {
					info, err := entry.Info()
					if err != nil {
						log.Errorf("failed to get file info for %s: %v", entryPath, err)
						continue
					}
					f.setFileState(entryPath, FileState{
						Size:        info.Size(),
						Unsupported: true,
					})
				}
				continue
			}

			// Check if file is too old
			if !entry.IsDir() {
				info, err := entry.Info()
				if err != nil {
					log.Errorf("failed to get file info for %s: %v", entryPath, err)
					continue
				}
				if info.ModTime().Before(time.Now().Add(-time.Duration(conf.SkipPeriodHours) * time.Hour)) {
					f.setFileState(entryPath, FileState{
						Size:        info.Size(),
						Unsupported: true,
						TooOld:      true,
					})
					continue
				}
			}

			isListening := newListenDirs.Contains(dir)
			isCollecting := newCollectDirs.Contains(dir)
			isPrevListening := f.listenDirs.Contains(dir)

			// Check if file needs to skip process
			curFileSize, err := handler.GetFileSize(entryPath)
			if err != nil {
				return fmt.Errorf("error getting file size for %s: %w", entryPath, err)
			}

			// Update state
			newState := fileState
			if !hasFileState || fileState.Size != curFileSize {
				startTime, endTime, err := handler.GetStartTimeEndTime(entryPath)
				if err != nil {
					log.Errorf("Error getting start and end time for %s: %v", entryPath, err)
					f.setFileState(entryPath, FileState{
						Size:        curFileSize,
						Unsupported: true,
					})
					continue
				}
				newState = FileState{
					Size:      curFileSize,
					StartTime: startTime.Unix(),
					EndTime:   endTime.Unix(),
				}
			}

			newState.IsListening = isListening
			newState.IsCollecting = isCollecting
			// change the process state to processed if the file is not being listened to all the time
			if !isListening || !isPrevListening {
				newState.ProcessState = processStateProcessed
			}

			f.setFileState(entryPath, newState)
		}
	}

	// Update directory sets
	f.srcDirs = newListenDirs.Union(newCollectDirs)
	f.listenDirs = newListenDirs

	// Clean up deleted files
	if err := f.updateDeletedFileState(); err != nil {
		return fmt.Errorf("failed to update deleted file state: %w", err)
	}

	// Save state to disk
	if err := f.saveState(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	log.Infof("Finished updating directories")
	return nil
}

func (f *fileStateHandler) UpdateFilesProcessState() error {
	for filename, state := range f.state {
		if !state.IsListening {
			continue
		}

		switch state.ProcessState {
		case processStateUnprocessed:
			state.ProcessState = processStateSeenOnce
		case processStateSeenOnce:
			state.ProcessState = processStateReadyToProcess
		default:
			continue
		}

		f.setFileState(filename, state)
	}
	if err := f.saveState(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

func (f *fileStateHandler) MarkProcessedFile(filename string) error {
	state, exists := f.getFileState(filename)
	if !exists {
		return fmt.Errorf("file state for %s does not exist", filename)
	}

	state.ProcessState = processStateProcessed

	f.setFileState(filename, state)

	if err := f.saveState(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// FileFilter type for file filtering
type FileFilter func(string, FileState) bool

// Files returns files matching the given filters
func (f *fileStateHandler) Files(filters ...FileFilter) []string {
	var result []string
	for filename, state := range f.state {
		if state.Unsupported {
			continue
		}

		matches := true
		for _, filter := range filters {
			if !filter(filename, state) {
				matches = false
				break
			}
		}

		if matches {
			result = append(result, filename)
		}
	}
	return result
}

// Filter factory functions

func StateReadyToProcessFilter() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsListening && state.ProcessState == processStateReadyToProcess
	}
}

func StateTimestampFilter(startTime, endTime int64) FileFilter {
	return func(_ string, state FileState) bool {
		return state.StartTime <= endTime && state.EndTime >= startTime
	}
}

func StateDirFilter(getDir bool) FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsDir == getDir
	}
}

func StateIsListeningFilter() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsListening
	}
}

func StateIsCollectingFilter() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsCollecting
	}
}
