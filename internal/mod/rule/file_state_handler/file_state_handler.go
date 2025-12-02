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

package file_state_handler

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod/rule/file_handlers"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

// FileStateHandler is the interface for the file state handler.
type FileStateHandler interface {
	// UpdateListenDirs updates the directories being monitored.
	UpdateListenDirs(conf config.DefaultModConfConfig) error

	// UpdateCollectDirs updates the directories being collected.
	UpdateCollectDirs(whitelist []string, conf config.DefaultModConfConfig) error

	// Files returns filename and file state pairs that match the given filters.
	Files(filters ...FileFilter) []FileState

	// UpdateFilesProcessState updates the state of files that are ready to process.
	// This should be called before processing files
	UpdateFilesProcessState() error

	// MarkProcessedFile updates the state of files that were ready to process
	// to mark them as processed.
	MarkProcessedFile(filename string) error

	// GetFileHandler returns the handler for a given file path.
	GetFileHandler(filePath string) file_handlers.Interface
}

// processState represents the state of a file in the processing pipeline.
type processState int

const (
	processStateUnprocessed processState = iota
	processStateSeenOnce
	processStateReadyToProcess
	processStateProcessed
)

// FileState represents the state of a file in the system.
type FileState struct {
	Size        int64 `json:"size"`
	StartTime   int64 `json:"start_time,omitempty"`
	EndTime     int64 `json:"end_time,omitempty"`
	ModifyTime  int64 `json:"modify_time,omitempty"`
	Unsupported bool  `json:"unsupported,omitempty"`

	TooOld       bool         `json:"too_old,omitempty"`
	IsCollecting bool         `json:"is_collecting,omitempty"`
	IsDir        bool         `json:"is_dir,omitempty"`
	IsListening  bool         `json:"is_listening,omitempty"`
	ProcessState processState `json:"process_state,omitempty"`
	Pathname     string       `json:"-"` // Pathname is output only for the Files method
}

// SavedState represents the complete state to be saved to disk.
type SavedState struct {
	State       map[string]FileState `json:"state"`
	ListenDirs  []string             `json:"listen_dirs"`
	CollectDirs []string             `json:"collect_dirs,omitempty"`
}

// fileStateHandler is used to keep track of the state of files in directories.
type fileStateHandler struct {
	state        map[string]FileState
	updateLock   sync.Mutex
	listenDirs   mapset.Set[string]
	collectDirs  mapset.Set[string]
	activeTopics mapset.Set[string]
	statePath    string
	handlers     []file_handlers.Interface
}

var (
	//nolint: gochecknoglobals // singleton instances map
	instances = make(map[string]*fileStateHandler)
	//nolint: gochecknoglobals // singleton mutex
	instancesMutex sync.Mutex
)

// New returns the singleton instance of fileStateHandler for the given fileStatePath.
// Each unique fileStatePath will have its own singleton instance.
func New(fileStatePath string) (FileStateHandler, error) {
	fullPath := path.Join(config.GetUserBaseFolder(), fileStatePath)

	instancesMutex.Lock()
	defer instancesMutex.Unlock()

	// Check if instance already exists for this path
	if instance, exists := instances[fullPath]; exists {
		return instance, nil
	}

	// Create new instance
	instance := &fileStateHandler{
		state:        make(map[string]FileState),
		collectDirs:  mapset.NewSet[string](),
		listenDirs:   mapset.NewSet[string](),
		activeTopics: mapset.NewSet[string](),
		statePath:    fullPath,
	}

	instance.registerHandlers()

	if err := instance.loadState(); err != nil {
		return nil, errors.Errorf("failed to initialize file state handler: %v", err)
	}

	// Store instance in map
	instances[fullPath] = instance

	return instance, nil
}

// registerHandlers registers the handlers for different file types.
func (f *fileStateHandler) registerHandlers() {
	// Register handlers here
	f.handlers = []file_handlers.Interface{
		file_handlers.NewLogHandler(),
		file_handlers.NewMcapHandler(),
		file_handlers.NewRos1Handler(),
	}
}

// GetFileHandler returns the handler for a given file path.
func (f *fileStateHandler) GetFileHandler(filePath string) file_handlers.Interface {
	for _, handler := range f.handlers {
		if handler.CheckFilePath(filePath) {
			return handler
		}
	}
	return nil
}

// LoadState loads the file state from disk.
func (f *fileStateHandler) loadState() error {
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Errorf("failed to read state file: %v", err)
	}

	savedState := SavedState{
		State: make(map[string]FileState),
	}
	err = json.Unmarshal(data, &savedState)
	if err != nil {
		log.Warnf("Invalid file state file: %v, reset to init", err)
	}

	f.state = savedState.State
	f.listenDirs = mapset.NewSet[string]()
	f.collectDirs = mapset.NewSet[string]()

	for _, dir := range savedState.ListenDirs {
		f.listenDirs.Add(dir)
	}
	for _, dir := range savedState.CollectDirs {
		f.collectDirs.Add(dir)
	}

	return nil
}

// SaveState saves the current state to disk.
func (f *fileStateHandler) saveState() error {
	savedState := SavedState{
		State:       f.state,
		ListenDirs:  f.listenDirs.ToSlice(),
		CollectDirs: f.collectDirs.ToSlice(),
	}

	data, err := json.MarshalIndent(savedState, "", "  ")
	if err != nil {
		return errors.Errorf("failed to marshal state: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(f.statePath), 0755); err != nil {
		return errors.Errorf("failed to create state directory: %v", err)
	}

	if err := os.WriteFile(f.statePath, data, 0600); err != nil {
		return errors.Errorf("failed to write state file: %v", err)
	}

	return nil
}

// setFileState updates the state for a given file path.
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

// delFileState removes the state for a given file path.
func (f *fileStateHandler) delFileState(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return errors.Errorf("failed to get absolute path for %s: %v", filePath, err)
	}

	f.updateLock.Lock()
	defer f.updateLock.Unlock()
	delete(f.state, absPath)
	return nil
}

// getFileState retrieves the state for a given file path.
func (f *fileStateHandler) getFileState(filePath string) (FileState, bool) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Errorf("Failed to get absolute path for %s: %v", filePath, err)
		return FileState{}, false
	}

	state, exists := f.state[absPath]
	return state, exists
}

// updateDeletedFileState removes state entries for files that no longer exist.
func (f *fileStateHandler) updateDeletedFileState() error {
	for _, filename := range lo.Keys(f.state) {
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			if err = f.delFileState(filename); err != nil {
				return errors.Errorf("failed to delete file state for %s: %v", filename, err)
			}
		} else if err != nil {
			return errors.Errorf("failed to stat file %s: %v", filename, err)
		}
	}
	return nil
}

func (f *fileStateHandler) UpdateListenDirs(conf config.DefaultModConfConfig) error {
	// Filter directories for read access and create sets
	newListenDirs := mapset.NewSet[string]()
	for _, dir := range conf.ListenDirs {
		if utils.CheckReadPath(dir) {
			newListenDirs.Add(dir)
		}
	}
	log.Infof("Start updating directories, listen_dirs: %v", newListenDirs)

	// Find directories that are no longer being monitored
	deleteDirs := f.listenDirs.Difference(newListenDirs)

	// Delete state of files in directories that are no longer scanned
	for filename := range f.state {
		for dir := range deleteDirs.Iter() {
			if strings.HasPrefix(filename, dir) {
				if err := f.delFileState(filename); err != nil {
					return errors.Errorf("failed to delete file state for %s: %v", filename, err)
				}
				log.Infof("Deleted file state for %s in directory %s", filename, dir)
			}
		}
	}

	log.Infof("Listening scan files modified in the last %d hours", conf.SkipPeriodHours)
	// Iterate over new directories and update file states
	for dir := range newListenDirs.Iter() {
		if fileInfo, err := os.Stat(dir); err != nil || !fileInfo.IsDir() {
			log.Warningf("%s is not a directory, skipping", dir)
			continue
		}

		// Check should recursively walk directories
		//nolint: nestif // complexity is acceptable for this function
		if conf.RecursivelyWalkDirs {
			filePaths, err := utils.GetAllFilePaths(dir, &utils.SymWalkOptions{
				FollowSymlinks:       true,
				SkipPermissionErrors: true,
				SkipEmptyFiles:       true,
				MaxFiles:             99999,
			})

			if err != nil {
				log.Errorf("Failed to get all file paths in directory %s, skipping: %v", dir, err)
				continue
			}

			for _, entryPath := range filePaths {
				time.Sleep(5 * time.Millisecond) // Sleep to prevent high CPU usage

				// Check if the entry is readable
				if !utils.CheckReadPath(entryPath) {
					log.Warnf("Skipping file %s due to insufficient permissions", entryPath)
					continue
				}

				// Get the entry info
				d, err := os.Stat(entryPath)
				if err != nil {
					log.Errorf("Failed to stat file %s, skipping: %v", entryPath, err)
					continue
				}

				// Process the file
				f.processListenFile(entryPath, d, conf.SkipPeriodHours)
			}
		} else {
			entries, err := os.ReadDir(dir)
			if err != nil {
				log.Errorf("Failed to read directory %s, skipping: %v", dir, err)
				continue
			}

			for _, entry := range entries {
				time.Sleep(5 * time.Millisecond) // Sleep to prevent high CPU usage

				if entry.IsDir() {
					continue
				}
				// Get absolute path of the entry
				absPath := filepath.Join(dir, entry.Name())
				if !utils.CheckReadPath(absPath) {
					log.Warnf("Skipping file %s due to insufficient permissions", absPath)
					continue
				}

				// Get file info
				info, err := entry.Info()
				if err != nil {
					log.Errorf("Failed to get file info for %s, skipping: %v", absPath, err)
					continue
				}

				// Process the file
				f.processListenFile(absPath, info, conf.SkipPeriodHours)
			}
		}
	}

	// Update directory sets
	f.listenDirs = newListenDirs

	// Clean up deleted files
	if err := f.updateDeletedFileState(); err != nil {
		return errors.Errorf("failed to update deleted file state: %v", err)
	}

	// Save state to disk
	if err := f.saveState(); err != nil {
		return errors.Errorf("failed to save state: %v", err)
	}

	log.Infof("Finished updating listen directories")
	return nil
}

func (f *fileStateHandler) UpdateCollectDirs(whitelist []string, conf config.DefaultModConfConfig) error {
	// Filter directories for read access and create sets
	newCollectDirs := mapset.NewSet[string]()
	for _, dir := range conf.CollectDirs {
		if utils.CheckReadPath(dir) {
			newCollectDirs.Add(dir)
		}
	}
	log.Infof("Start updating directories, collector_dirs: %v", newCollectDirs)

	// Find directories that are no longer being monitored
	deleteDirs := f.collectDirs.Difference(newCollectDirs)

	// Delete state of files in directories that are no longer collected
	for filename := range f.state {
		for dir := range deleteDirs.Iter() {
			if strings.HasPrefix(filename, dir) {
				if err := f.delFileState(filename); err != nil {
					return errors.Errorf("failed to delete file state for %s: %v", filename, err)
				}
				log.Infof("Deleted file state for %s in directory %s", filename, dir)
			}
		}
	}

	// Helper function to check if file matches whitelist
	matchesWhitelist := func(filename string) bool {
		if len(whitelist) == 0 {
			return true
		}
		for _, pattern := range whitelist {
			matched, err := doublestar.PathMatch(pattern, filename)
			if err == nil && matched {
				return true
			}
		}
		return false
	}

	// Iterate over new directories and update file states
	for dir := range newCollectDirs.Iter() {
		if fileInfo, err := os.Stat(dir); err != nil || !fileInfo.IsDir() {
			log.Warningf("%s is not a directory, skipping", dir)
			continue
		}

		// Check should recursively walk directories
		//nolint: nestif // complexity is acceptable for this function
		if conf.RecursivelyWalkDirs {
			filePaths, err := utils.GetAllFilePaths(dir, &utils.SymWalkOptions{
				FollowSymlinks:       true,
				SkipPermissionErrors: true,
				SkipEmptyFiles:       true,
				MaxFiles:             99999,
			})

			if err != nil {
				log.Errorf("Failed to get all file paths in directory %s, skipping: %v", dir, err)
				continue
			}

			for _, entryPath := range filePaths {
				time.Sleep(5 * time.Millisecond) // Sleep to prevent high CPU usage

				// Check whitelist before processing (saves expensive file operations)
				if !matchesWhitelist(entryPath) {
					continue
				}

				// Check if the entry is readable
				if !utils.CheckReadPath(entryPath) {
					log.Warnf("Skipping file %s due to insufficient permissions", entryPath)
					return nil
				}

				// Get the entry info
				d, err := os.Stat(entryPath)
				if err != nil {
					log.Errorf("Failed to stat file %s, skipping: %v", entryPath, err)
					continue
				}

				// Process the file
				f.processCollectFile(entryPath, d)
			}
		} else {
			entries, err := os.ReadDir(dir)
			if err != nil {
				log.Errorf("Failed to read directory %s, skipping: %v", dir, err)
				continue
			}

			for _, entry := range entries {
				time.Sleep(5 * time.Millisecond) // Sleep to prevent high CPU usage

				if entry.IsDir() {
					continue
				}
				// Get absolute path of the entry
				// Use filepath.Abs to ensure consistency with recursive branch and Files() method
				joinedPath := filepath.Join(dir, entry.Name())
				absPath, err := filepath.Abs(joinedPath)
				if err != nil {
					log.Errorf("Failed to get absolute path for %s, skipping: %v", joinedPath, err)
					continue
				}

				// Check whitelist before processing (saves expensive file operations)
				if !matchesWhitelist(absPath) {
					continue
				}

				if !utils.CheckReadPath(absPath) {
					log.Warnf("Skipping file %s due to insufficient permissions", absPath)
					continue
				}

				// Get file info
				info, err := entry.Info()
				if err != nil {
					log.Errorf("Failed to get file info for %s, skipping: %v", absPath, err)
					continue
				}

				// Process the file
				f.processCollectFile(absPath, info)
			}
		}
	}

	// Update directory sets
	f.collectDirs = newCollectDirs

	// Clean up deleted files
	if err := f.updateDeletedFileState(); err != nil {
		return errors.Errorf("failed to update deleted file state: %v", err)
	}

	// Save state to disk
	if err := f.saveState(); err != nil {
		return errors.Errorf("failed to save state: %v", err)
	}

	log.Infof("Finished updating collect directories")
	return nil
}

func (f *fileStateHandler) processListenFile(absPath string, info os.FileInfo, skipPeriodHours int) {
	fileState, hasFileState := f.getFileState(absPath)

	// Skip already processed files
	if hasFileState && !fileState.Unsupported && fileState.ProcessState == processStateProcessed {
		return
	}

	handler := f.GetFileHandler(absPath)
	if handler == nil {
		// No handler supported for file, mark as unsupported if not already
		if !hasFileState || !fileState.Unsupported {
			f.setFileState(absPath, FileState{
				Size:        info.Size(),
				Unsupported: true,
			})
		}
		return
	}

	// Check if file is too old
	if !info.IsDir() && info.ModTime().Before(time.Now().Add(-time.Duration(skipPeriodHours)*time.Hour)) {
		f.setFileState(absPath, FileState{
			Size:        info.Size(),
			Unsupported: true,
			TooOld:      true,
		})
		return
	}

	if handler.IsFinished(absPath) {
		// Update state
		newState := fileState
		if !hasFileState {
			newState = FileState{}
		}

		newState.Size = info.Size()
		newState.ModifyTime = info.ModTime().Unix()
		newState.IsListening = true
		newState.Unsupported = false
		newState.TooOld = false
		f.setFileState(absPath, newState)
		return
	} else {
		log.Infof("Listening file %s is not finished, will be processed later", absPath)
	}
}

func (f *fileStateHandler) processCollectFile(absPath string, info os.FileInfo) {
	fileState, hasFileState := f.getFileState(absPath)

	// Skip file if file is not modified
	if hasFileState &&
		!fileState.Unsupported &&
		fileState.ProcessState == processStateProcessed &&
		fileState.ModifyTime == info.ModTime().Unix() {
		return
	}

	handler := f.GetFileHandler(absPath)
	if handler == nil {
		f.setFileState(absPath, FileState{
			Size:        info.Size(),
			ModifyTime:  info.ModTime().Unix(),
			Unsupported: true,
		})
		return
	}

	// Update state
	newState := fileState
	if !hasFileState || fileState.ModifyTime != info.ModTime().Unix() {
		startTime, endTime, err := handler.GetStartTimeEndTime(absPath)
		if err != nil {
			log.Errorf("Error getting start and end time for %s: %v, skip collect", absPath, err)

			f.setFileState(absPath, FileState{
				Size:        info.Size(),
				ModifyTime:  info.ModTime().Unix(),
				Unsupported: true,
			})
			return
		}
		newState = FileState{
			Size:       info.Size(),
			StartTime:  startTime.Unix(),
			EndTime:    endTime.Unix(),
			ModifyTime: info.ModTime().Unix(),
		}
	}

	newState.IsCollecting = true
	newState.ProcessState = processStateProcessed

	f.setFileState(absPath, newState)
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
		case processStateReadyToProcess, processStateProcessed:
			continue
		}

		f.setFileState(filename, state)
	}
	if err := f.saveState(); err != nil {
		return errors.Errorf("failed to save state: %v", err)
	}

	return nil
}

func (f *fileStateHandler) MarkProcessedFile(filename string) error {
	state, exists := f.getFileState(filename)
	if !exists {
		return errors.Errorf("file state for %s does not exist", filename)
	}

	state.ProcessState = processStateProcessed

	f.setFileState(filename, state)

	if err := f.saveState(); err != nil {
		return errors.Errorf("failed to save state: %v", err)
	}

	return nil
}

// FileFilter type for file filtering.
type FileFilter func(string, FileState) bool

// Files returns files matching the given filters.
func (f *fileStateHandler) Files(filters ...FileFilter) []FileState {
	f.updateLock.Lock()
	defer f.updateLock.Unlock()

	var result []FileState
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
			state.Pathname = filename
			result = append(result, state)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTime < result[j].StartTime ||
			result[i].EndTime < result[j].EndTime ||
			result[i].Pathname < result[j].Pathname
	})
	return result
}

// Filter factory functions.

// FilterReadyToProcess returns a filter that matches files that are ready to process.
func FilterReadyToProcess() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsListening && state.ProcessState == processStateReadyToProcess
	}
}

// FilterTime returns a filter that matches files that have intervals overlapping with the given time.
func FilterTime(startTime, endTime int64) FileFilter {
	return func(_ string, state FileState) bool {
		return state.StartTime <= endTime && state.EndTime >= startTime
	}
}

// FilterIsListening returns a filter that matches files that are being listened to.
func FilterIsListening() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsListening
	}
}

// FilterIsCollecting returns a filter that matches files that are being collected.
func FilterIsCollecting() FileFilter {
	return func(_ string, state FileState) bool {
		return state.IsCollecting
	}
}
