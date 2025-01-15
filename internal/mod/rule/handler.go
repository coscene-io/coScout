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

package rule

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const checkInterval = 60 * time.Second

type CustomRuleHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
}

func NewRuleHandler(reqClient api.RequestClient, confManager config.ConfManager, errChan chan error) *CustomRuleHandler {
	return &CustomRuleHandler{
		reqClient:   reqClient,
		confManager: confManager,
		errChan:     errChan,
	}
}

func (c CustomRuleHandler) Run(ctx context.Context) {
	listenChan := make(chan string, 1000)
	modConfig := &config.DefaultModConfConfig{SkipPeriodHours: 2}

	// Create a channel to signal when fileStateHandler is ready
	handlerReady := make(chan file_state_handler.FileStateHandler)

	// start a periodic goroutine to
	// 1. load the config
	// 2. update the file state handler
	// 3. send files to be processed
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				appConfig := c.confManager.LoadWithRemote()
				confConfig, ok := appConfig.Mod.Config.(config.DefaultModConfConfig)
				if ok {
					*modConfig = confConfig
				}

				fileStateHandler, err := file_state_handler.New()
				if err != nil {
					log.Errorf("create file state handler: %v", err)
					c.errChan <- errors.Wrap(err, "create file state handler")
					break
				}

				// Send the initialized handler through the channel on first creation
				select {
				case handlerReady <- fileStateHandler:
				default:
				}

				c.sendFilesToBeProcessed(listenChan, fileStateHandler, modConfig)
			case <-ctx.Done():
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

	// Wait for the initial fileStateHandler to be ready
	fileStateHandler := <-handlerReady

	// Now start other goroutines with the initialized handler
	go c.handleListenFiles(ctx, fileStateHandler, listenChan)

	// Now start the collect info handler periodic goroutine
	collectTicker := time.NewTicker(1 * time.Second)
	defer collectTicker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				c.scanCollectInfosAndHandle(fileStateHandler)
			case <-ctx.Done():
				return
			}
			t.Reset(checkInterval)
		}
	}(collectTicker)

	<-ctx.Done()
	log.Infof("Rule handler stopped")
}

func (c CustomRuleHandler) sendFilesToBeProcessed(
	listenChan chan string,
	fileStateHandler file_state_handler.FileStateHandler,
	modConfig *config.DefaultModConfConfig,
) {
	if len(modConfig.CollectDirs) == 0 {
		return
	}

	if err := fileStateHandler.UpdateDirs(*modConfig); err != nil {
		log.Errorf("file state handler update dirs: %v", err)
		return
	}

	if err := fileStateHandler.UpdateFilesProcessState(); err != nil {
		log.Errorf("file state handler update process state: %v", err)
		return
	}

	for _, fileState := range fileStateHandler.Files(file_state_handler.StateReadyToProcessFilter()) {
		listenChan <- fileState.Pathname
	}
}

func (c CustomRuleHandler) handleListenFiles(
	ctx context.Context,
	fileStateHandler file_state_handler.FileStateHandler,
	listenChan chan string,
) {
	// semaphore is used to control the number of concurrent processing files
	semaphore := make(chan struct{}, 2)
	var wg sync.WaitGroup

	for {
		select {
		case fileToProcess, ok := <-listenChan:
			if !ok {
				wg.Wait()
				return
			}

			semaphore <- struct{}{}

			wg.Add(1)
			go func(filename string) {
				defer wg.Done()
				defer func() { <-semaphore }() // 释放信号量

				c.processFileWithRule(filename)
				log.Infof("Finished processing file: %v", filename)

				err := fileStateHandler.MarkProcessedFile(filename)
				if err != nil {
					log.Errorf("mark processed file: %v", err)
				}
			}(fileToProcess)
		case <-ctx.Done():
			wg.Wait()
			return
		}
	}
}

func (c CustomRuleHandler) processFileWithRule(
	filename string,
) {
	log.Infof("RuleEngine exec file: %v", filename)
	// todo(shuhao): handle file with rule
}

// scanCollectInfosAndHandle periodically checks the collect info dir and handles the collect info.
func (c CustomRuleHandler) scanCollectInfosAndHandle(fileStateHandler file_state_handler.FileStateHandler) {
	log.Infof("Starts to scan collect info dir")

	// Search for files under the collect info dir and handles them
	collectInfoDir := config.GetCollectInfoFolder()
	log.Infof("Collect info dir: %v", collectInfoDir)

	entries, err := os.ReadDir(collectInfoDir)
	if err != nil {
		log.Errorf("read collect info dir: %v", err)
		c.errChan <- errors.Wrap(err, "read collect info dir")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		collectInfoId := strings.TrimSuffix(entry.Name(), ".json")

		collectInfo := &model.CollectInfo{}
		if err := collectInfo.Load(collectInfoId); err != nil {
			log.Errorf("load collect info: %v", err)
			c.errChan <- errors.Wrap(err, "load collect info")
			continue
		}

		// todo(shuhao): handle collect info
		c.handleCollectInfo(fileStateHandler, *collectInfo)
	}
}

// handleCollectInfo handles the collect info.
func (c CustomRuleHandler) handleCollectInfo(fileStateHandler file_state_handler.FileStateHandler, info model.CollectInfo) {
	if info.Cut == nil || time.Unix(info.Cut.End, 0).After(time.Now()) {
		return
	}

	log.Infof("Collecting for files start: %v, end: %v", info.Cut.Start, info.Cut.End)

	// Get files
	uploadWhiteListFilter := func(filename string, _ file_state_handler.FileState) bool {
		if len(info.Cut.WhiteList) == 0 {
			return true
		}
		for _, pattern := range info.Cut.WhiteList {
			matched, err := filepath.Match(pattern, filename)
			if err == nil && matched {
				return true
			}
		}
		return false
	}
	uploadFileStates := fileStateHandler.Files(
		file_state_handler.StateIsCollectingFilter(),
		file_state_handler.StateTimestampFilter(info.Cut.Start, info.Cut.End),
		uploadWhiteListFilter,
	)

	for _, extraFileRaw := range info.Cut.ExtraFiles {
		extraFileAbs, err := filepath.Abs(extraFileRaw)
		if err != nil {
			log.Errorf("get abs path for extra file: %v", err)
			continue
		}

		if utils.CheckReadPath(extraFileAbs) {
			continue
		}

		stat, err := os.Stat(extraFileAbs)
		if err != nil {
			log.Errorf("stat extra file failed after check read path: %v", err)
			continue
		}

		uploadFileStates = append(uploadFileStates, file_state_handler.FileState{
			Size:     stat.Size(),
			IsDir:    stat.IsDir(),
			Pathname: extraFileAbs,
		})
	}

	log.Infof("finish collecting for files start: %v, end: %v", info.Cut.Start, info.Cut.End)

	rc := model.RecordCache{
		ProjectName:   info.ProjectName,
		Record:        info.Record,
		Labels:        info.Labels,
		Timestamp:     time.Now().UnixMilli(),
		DiagnosisTask: info.DiagnosisTask,
		OriginalFiles: computeFileInfos(uploadFileStates),
	}

	for _, moment := range info.Moments {
		ts := moment.Timestamp
		// Convert to milliseconds if the timestamp is in seconds
		if ts > 1_000_000_000_000 {
			ts /= 1_000
		}
		startTime := moment.StartTime
		if startTime > 1_000_000_000_000 {
			startTime /= 1_000
		}
		duration := ts - startTime

		momentToCreate := model.Moment{
			Title:       moment.Title,
			Description: moment.Description,
			Timestamp:   ts,
			Duration:    duration,
			Code:        moment.Code,
			RuleId:      moment.RuleId,
		}
		if moment.CreateTask {
			momentToCreate.Task = model.Task{
				Title:       moment.Title,
				Description: moment.Description,
				Assignee:    moment.AssignTo,
				SyncTask:    moment.SyncTask,
			}
		}
		rc.Moments = append(rc.Moments, momentToCreate)
	}

	if err := rc.Save(); err != nil {
		log.Errorf("save record cache: %v", err)
	}

	if cleanId := info.Clean(); cleanId == "" {
		log.Errorf("clean collect info failed for id: %v", info.Id)
	}
}

// computeFileInfos computes the fileInfos given the fileStates.
func computeFileInfos(fileStates []file_state_handler.FileState) map[string]model.FileInfo {
	files := make(map[string]model.FileInfo)
	for _, fileState := range fileStates {
		if !fileState.IsDir {
			baseName := filepath.Base(fileState.Pathname)
			switch {
			case strings.HasSuffix(baseName, ".bag"):
				files[fileState.Pathname] = model.FileInfo{
					Path:     fileState.Pathname,
					Size:     fileState.Size,
					FileName: path.Join("bag", baseName),
				}
				continue
			case strings.HasSuffix(baseName, ".log"):
				files[fileState.Pathname] = model.FileInfo{
					Path:     fileState.Pathname,
					Size:     fileState.Size,
					FileName: path.Join("log", baseName),
				}
				continue
			default:
				files[fileState.Pathname] = model.FileInfo{
					Path:     fileState.Pathname,
					Size:     fileState.Size,
					FileName: path.Join("files", baseName),
				}
				continue
			}
		}

		baseDir := filepath.Dir(fileState.Pathname)

		err := filepath.Walk(fileState.Pathname, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Errorf("failed to walk through path %s", path)
				//nolint: nilerr // continue walking
				return nil
			}

			if info.IsDir() {
				return nil
			}

			filename, err := filepath.Rel(baseDir, path)
			if err != nil {
				log.Errorf("failed to get relative path: %v", err)
				filename = filepath.Base(path)
			}

			files[path] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     path,
			}
			return nil
		})
		log.Errorf("failed to walk through dir %s: %v", fileState.Pathname, err)
	}
	return files
}
