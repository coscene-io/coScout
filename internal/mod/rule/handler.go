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
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	gcmessage "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	// numThreadToProcessFile is the number of threads to process files concurrently.
	numThreadToProcessFile = 2
)

type CustomRuleHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
	pubSub      *gochannel.GoChannel

	fileStateHandler file_state_handler.FileStateHandler
	listenChan       chan string
	ruleItemChan     chan rule_engine.RuleItem
	engine           Engine
}

func NewRuleHandler(reqClient api.RequestClient, confManager config.ConfManager, pubSub *gochannel.GoChannel, errChan chan error) *CustomRuleHandler {
	fileStateHandler, err := file_state_handler.New()
	if err != nil {
		log.Errorf("create file state handler: %v", err)
		errChan <- errors.Wrap(err, "create file state handler")
	}
	return &CustomRuleHandler{
		reqClient:        reqClient,
		confManager:      confManager,
		errChan:          errChan,
		fileStateHandler: fileStateHandler,
		listenChan:       make(chan string, 1000),
		ruleItemChan:     make(chan rule_engine.RuleItem, 1000),
		engine:           Engine{},
		pubSub:           pubSub,
	}
}

func (c *CustomRuleHandler) Run(ctx context.Context) {
	if c.fileStateHandler == nil {
		log.Errorf("file state handler is nil")
		return
	}

	modConfig := &config.DefaultModConfConfig{}

	// start a periodic goroutine to load the config and update the rules in rule engine
	modFirstUpdated := make(chan struct{})
	configTicker := time.NewTicker(1 * time.Second)
	defer configTicker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				configTicker.Reset(config.ReloadRulesInterval)

				appConfig := c.confManager.LoadWithRemote()
				confConfig, ok := appConfig.Mod.Config.(config.DefaultModConfConfig)
				if ok {
					*modConfig = confConfig
				}

				device := core.GetDeviceInfo(c.confManager.GetStorage())
				if device == nil {
					log.Errorf("get device info failed")
					continue
				}

				//nolint: contextcheck// context is checked in the parent goroutine
				apiRules, err := c.reqClient.ListDeviceDiagnosisRules(device.GetName())
				if err != nil {
					log.Errorf("list device diagnosis rules: %v", err)
					c.errChan <- errors.Errorf("list device diagnosis rules: %v", err)
					continue
				}
				log.Infof("received rules: %d", len(apiRules))

				c.engine.UpdateRules(apiRules, appConfig.Topics)
				log.Infof("handling topics: %v", c.engine.ActiveTopics())

				select {
				case modFirstUpdated <- struct{}{}:
				default:
				}
			case <-ctx.Done():
				return
			}
		}
	}(configTicker)
	<-modFirstUpdated

	// start a periodic goroutine to search for files to be processed and send to listenChan
	listenTicker := time.NewTicker(1 * time.Second)
	defer listenTicker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				listenTicker.Reset(config.RuleCheckListenFilesInterval)

				c.sendFilesToBeProcessed(modConfig)
			case <-ctx.Done():
				return
			}
		}
	}(listenTicker)

	// start goroutine process to concurrently process listened files and send messages
	go c.processListenedFilesAndSendMessages(ctx, numThreadToProcessFile)

	// start goroutine process to consume messages using rule engine
	go func() {
		for {
			select {
			case ruleItem := <-c.ruleItemChan:
				c.engine.ConsumeNext(ruleItem)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Now start the collect info handler periodic goroutine
	collectTicker := time.NewTicker(1 * time.Second)
	defer collectTicker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				t.Reset(config.RuleScanCollectInfosInterval)

				c.scanCollectInfosAndHandle()
			case <-ctx.Done():
				return
			}
		}
	}(collectTicker)

	go c.handleSubMsg(ctx)

	<-ctx.Done()
	log.Infof("Rule handler stopped")
}

func (c *CustomRuleHandler) handleSubMsg(ctx context.Context) {
	messages, err := c.pubSub.Subscribe(ctx, constant.TopicRuleMsg)
	if err != nil {
		log.Errorf("subscribe to rule message: %v", err)
		c.errChan <- errors.Wrap(err, "subscribe to rule message")
		return
	}

	for {
		select {
		case msg := <-messages:
			item := rule_engine.RuleItem{}
			err := json.Unmarshal(msg.Payload, &item)
			if err != nil {
				log.Errorf("unmarshal rule item: %v", err)

				msg.Ack()
				continue
			}

			c.ruleItemChan <- item
			msg.Ack()
		case <-ctx.Done():
			return
		}
	}
}

func (c *CustomRuleHandler) sendFilesToBeProcessed(modConfig *config.DefaultModConfConfig) {
	if len(modConfig.CollectDirs) == 0 {
		return
	}

	if err := c.fileStateHandler.UpdateDirs(*modConfig); err != nil {
		log.Errorf("file state handler update dirs: %v", err)
		return
	}

	if err := c.fileStateHandler.UpdateFilesProcessState(); err != nil {
		log.Errorf("file state handler update process state: %v", err)
		return
	}

	for _, fileState := range c.fileStateHandler.Files(
		file_state_handler.FilterIsListening(),
		file_state_handler.FilterReadyToProcess(),
	) {
		err := c.fileStateHandler.MarkProcessedFile(fileState.Pathname)
		if err != nil {
			log.Errorf("mark processed file: %v", err)
		}
		c.listenChan <- fileState.Pathname
	}
}

func (c *CustomRuleHandler) processListenedFilesAndSendMessages(
	ctx context.Context,
	numThreadToProcessFile int,
) {
	// semaphore is used to control the number of concurrent processing files
	semaphore := make(chan struct{}, numThreadToProcessFile)
	var wg sync.WaitGroup

	for {
		select {
		case fileToProcess, ok := <-c.listenChan:
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
			}(fileToProcess)
		case <-ctx.Done():
			wg.Wait()
			return
		}
	}
}

func (c *CustomRuleHandler) processFileWithRule(
	filename string,
) {
	log.Infof("RuleEngine exec file: %v", filename)
	handler := c.fileStateHandler.GetFileHandler(filename)
	if handler == nil {
		// this should not happen
		log.Errorf("get file handler failed for file: %v", filename)
		return
	}

	handler.SendRuleItems(filename, c.engine.ActiveTopics(), c.ruleItemChan)
}

// scanCollectInfosAndHandle handles all collect info files within the collect info dir.
func (c *CustomRuleHandler) scanCollectInfosAndHandle() {
	log.Infof("Starts to scan collect info dir")

	// Search for files under the collect info dir and handles them
	collectInfoDir := config.GetCollectInfoFolder()

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

		c.handleCollectInfo(*collectInfo)
	}
}

// handleCollectInfo handles a single the collect info.
func (c *CustomRuleHandler) handleCollectInfo(info model.CollectInfo) {
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
			matched, err := doublestar.PathMatch(pattern, filename)
			if err == nil && matched {
				return true
			}
		}
		return false
	}
	uploadFileStates := c.fileStateHandler.Files(
		file_state_handler.FilterIsCollecting(),
		file_state_handler.FilterTime(info.Cut.Start, info.Cut.End),
		uploadWhiteListFilter,
	)

	for _, extraFileRaw := range info.Cut.ExtraFiles {
		extraFileAbs, err := filepath.Abs(extraFileRaw)
		if err != nil {
			log.Errorf("get abs path for extra file: %v", err)
			continue
		}

		if !utils.CheckReadPath(extraFileAbs) {
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
		// Convert to seconds if the timestamp is in milliseconds
		if ts > 1_000_000_000_000 {
			ts /= 1_000
		}
		startTime := moment.StartTime
		if startTime > 1_000_000_000_000 {
			startTime /= 1_000
		}
		duration := ts - startTime

		ruleName, ok := info.DiagnosisTask["rule_name"].(string)
		if !ok {
			log.Errorf("rule_name is not a string")
			ruleName = ""
		}
		momentToCreate := model.Moment{
			Title:       moment.Title,
			Description: moment.Description,
			Timestamp:   ts,
			Duration:    duration,
			Code:        moment.Code,
			RuleName:    ruleName,
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

	msg := gcmessage.NewMessage(watermill.NewUUID(), []byte(rc.GetRecordCachePath()))
	err := c.pubSub.Publish(constant.TopicCollectMsg, msg)
	if err != nil {
		log.Errorf("Failed to publish collect message: %v", err)
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
