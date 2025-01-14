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
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/pkg/errors"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	log "github.com/sirupsen/logrus"
)

const checkInterval = 60 * time.Second

type CollectCutFileInfo struct {
	extraFiles []string
	// time in seconds
	start int64
	// time in seconds
	end       int64
	whiteList []string
}

type CollectFileInfo struct {
	projectName   string
	record        map[string]interface{}
	diagnosisTask map[string]interface{}
	cut           CollectCutFileInfo
}

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
	collectChan := make(chan CollectFileInfo, 1000)
	modConfig := &config.DefaultModConfConfig{SkipPeriodHours: 2}

	// Create a channel to signal when fileStateHandler is ready
	handlerReady := make(chan file_state_handler.FileStateHandler)

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
	go c.handleListenFiles(ctx, fileStateHandler, listenChan, collectChan)
	go c.handleCollectFiles(ctx, collectChan)

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
		c.errChan <- errors.Wrap(err, "file state handler update dirs")
		return
	}

	if err := fileStateHandler.UpdateFilesProcessState(); err != nil {
		log.Errorf("file state handler update process state: %v", err)
		c.errChan <- errors.Wrap(err, "file state handler update process state")
		return
	}

	for _, filename := range fileStateHandler.Files(file_state_handler.StateReadyToProcessFilter()) {
		listenChan <- filename
	}
}

func (c CustomRuleHandler) handleListenFiles(
	ctx context.Context,
	fileStateHandler file_state_handler.FileStateHandler,
	listenChan chan string,
	collectChan chan CollectFileInfo,
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

				c.processFileWithRule(filename, collectChan)
				log.Infof("Finished processing file: %v", filename)

				err := fileStateHandler.MarkProcessedFile(filename)
				if err != nil {
					log.Errorf("mark processed file: %v", err)
					c.errChan <- errors.Wrap(err, "mark processed file")
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
	collectChan chan CollectFileInfo,
) {
	// TODO(shuhao): collect file with ruleEngine
	log.Infof("RuleEngine exec file: %v", filename)

	collectChan <- CollectFileInfo{
		projectName: "test",
	}
}

func (c CustomRuleHandler) handleCollectFiles(ctx context.Context, collectChan chan CollectFileInfo) {
	for {
		select {
		case collectFileInfo, ok := <-collectChan:
			if !ok {
				return
			}

			log.Infof("Collect file: %v", collectFileInfo)
			// TODO(shuhao): handle collect file
		case <-ctx.Done():
			return
		}
	}
}
