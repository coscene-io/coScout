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
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	// numThreadToProcessFile is the number of threads to process files concurrently.
	numThreadToProcessFile = 2
)

const (
	// listen file state handler path.
	listenFileStatePath = "listenFile.state.json"

	// collect file state path.
	collectFileStatePath = "collectFile.state.json"
)

type CustomRuleHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
	pubSub      *gochannel.GoChannel

	listenFileStateHandler  file_state_handler.FileStateHandler
	collectFileStateHandler file_state_handler.FileStateHandler
	listenChan              chan string
	ruleItemChan            chan rule_engine.RuleItem
	engine                  Engine

	// Master-slave components (optional)
	slaveRegistry *master.SlaveRegistry
	masterClient  *master.Client
	masterConfig  *config.MasterConfig
}

func NewRuleHandler(reqClient api.RequestClient, confManager config.ConfManager, pubSub *gochannel.GoChannel, errChan chan error) *CustomRuleHandler {
	listenFileStateHandler, err := file_state_handler.New(listenFileStatePath)
	if err != nil {
		log.Errorf("create file state handler: %v", err)
		errChan <- errors.Wrap(err, "create file state handler")
		return nil
	}

	collectFileStateHandler, err := file_state_handler.New(collectFileStatePath)
	if err != nil {
		log.Errorf("create file state handler: %v", err)
		errChan <- errors.Wrap(err, "create file state handler")
	}

	return &CustomRuleHandler{
		reqClient:               reqClient,
		confManager:             confManager,
		errChan:                 errChan,
		listenFileStateHandler:  listenFileStateHandler,
		collectFileStateHandler: collectFileStateHandler,
		listenChan:              make(chan string, 1000),
		ruleItemChan:            make(chan rule_engine.RuleItem, 1000),
		engine:                  Engine{reqClient: reqClient, ruleDebounceTime: make(map[string]*time.Time)},
		pubSub:                  pubSub,
	}
}

func (c *CustomRuleHandler) Run(ctx context.Context) {
	if c.listenFileStateHandler == nil {
		log.Errorf("listen file state handler is nil")
		return
	}
	if c.collectFileStateHandler == nil {
		log.Errorf("collect file state handler is nil")
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
				c.engine.deviceName = device.GetName()

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

				topicBytes, err := json.Marshal(c.engine.ActiveTopics())
				if err == nil {
					msg := gcmessage.NewMessage(watermill.NewUUID(), topicBytes)
					if err := c.pubSub.Publish(constant.TopicConfigTopicsMsg, msg); err != nil {
						log.Errorf("Failed to publish message: %v", err)
					}
				} else {
					log.Errorf("Failed to marshal active topics: %v", err)
				}

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
				log.Infof("rule item channel closed")
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

				//nolint: contextcheck// context is checked in the parent goroutine
				c.scanCollectInfosAndHandle(modConfig)
			case <-ctx.Done():
				log.Infof("collect info handler stopped")
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
			if msg == nil {
				log.Warn("received nil message")
				continue
			}

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
	if len(modConfig.ListenDirs) == 0 {
		return
	}

	if err := c.listenFileStateHandler.UpdateListenDirs(*modConfig); err != nil {
		log.Errorf("file state handler update dirs: %v", err)
		return
	}

	if err := c.listenFileStateHandler.UpdateFilesProcessState(); err != nil {
		log.Errorf("file state handler update process state: %v", err)
		return
	}

	for _, fileState := range c.listenFileStateHandler.Files(
		file_state_handler.FilterIsListening(),
		file_state_handler.FilterReadyToProcess(),
	) {
		err := c.listenFileStateHandler.MarkProcessedFile(fileState.Pathname)
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
				defer func() { <-semaphore }() // release the semaphore

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
	handler := c.listenFileStateHandler.GetFileHandler(filename)
	if handler == nil {
		// this should not happen
		log.Errorf("get file handler failed for file: %v", filename)
		return
	}

	handler.SendRuleItems(filename, c.engine.ActiveTopics(), c.ruleItemChan)
}

// scanCollectInfosAndHandle handles all collect info files within the collect info dir.
func (c *CustomRuleHandler) scanCollectInfosAndHandle(modConfig *config.DefaultModConfConfig) {
	log.Infof("Starts to scan collect info dir")

	// Search for files under the collect info dir and handles them
	collectInfoDir := config.GetCollectInfoFolder()
	entries, err := os.ReadDir(collectInfoDir)
	if err != nil {
		log.Errorf("read collect info dir: %v", err)
		c.errChan <- errors.Wrap(err, "read collect info dir")
		return
	}

	collectInfoIds := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		collectInfoId := strings.TrimSuffix(entry.Name(), ".json")
		collectInfoIds = append(collectInfoIds, collectInfoId)
	}

	if len(collectInfoIds) == 0 {
		log.Infof("Finished scanning collect info dir, found 0 collect info files")
		return
	}

	log.Infof("Found %d collect info files", len(collectInfoIds))
	for _, collectInfoId := range collectInfoIds {
		collectInfo := &model.CollectInfo{}
		if err := collectInfo.Load(collectInfoId); err != nil {
			log.Errorf("load collect info: %v", err)
			c.errChan <- errors.Wrap(err, "load collect info")
			continue
		}

		// Skip early checks
		if collectInfo.Skip {
			log.WithField("collectID", collectInfo.Id).Infof("Skipping collect info: %v, cleaning", collectInfo.Id)
			collectInfo.Clean()
			continue
		}

		if collectInfo.Cut == nil {
			log.Errorf("Collect info cut is nil: %v", collectInfo.Id)
			collectInfo.Clean()
			continue
		}

		if time.Unix(collectInfo.Cut.End, 0).After(time.Now()) {
			log.WithField("collectID", collectInfo.Id).Infof("Collect info end time is not reached, skip!")
			continue
		}

		log.WithField("collectID", collectInfo.Id).Infof("Found collect info to handle")
		c.handleCollectInfo(*collectInfo, *modConfig)
	}
	log.Infof("Finished scanning collect info dir, found %d collect info files", len(collectInfoIds))
}

// handleCollectInfo handles a single the collect info.
func (c *CustomRuleHandler) handleCollectInfo(info model.CollectInfo, modConfig config.DefaultModConfConfig) {
	if info.Skip {
		log.WithField("collectID", info.Id).Infof("Skipping collect info, cleaning")
		info.Clean()
		return
	}

	if info.Cut == nil {
		log.WithField("collectID", info.Id).Errorf("Collect info cut is nil, cleaning")
		info.Clean()
		return
	}

	if time.Unix(info.Cut.End, 0).After(time.Now()) {
		log.WithField("collectID", info.Id).Infof("Collect info is not reached, skip!")
		return
	}

	log.WithField("collectID", info.Id).Infof("Collecting for files start: %v, end: %v", info.Cut.Start, info.Cut.End)
	recordTitle, _ := info.Record["title"].(string)

	uploadFileStates := make([]file_state_handler.FileState, 0)
	hasBirthTime := utils.HasBirthTime(modConfig.CollectDirs)
	//nolint: nestif // nested if is more readable here.
	if hasBirthTime {
		log.WithField("collectID", info.Id).Infof("At least one collect dir has birth time support")

		uploadFiles, noPermissionFiles := upload.ComputeUploadFiles(info.Id, modConfig.CollectDirs, []string{}, info.Cut.WhiteList, modConfig.RecursivelyWalkDirs, info.Cut.Start, info.Cut.End)
		if len(noPermissionFiles) > 0 {
			log.WithField("collectID", info.Id).Infof("Found no permission files: %v", noPermissionFiles)
		}
		for _, file := range uploadFiles {
			uploadFileStates = append(uploadFileStates, file_state_handler.FileState{
				Size:     file.Size,
				Pathname: file.Path,
				IsDir:    false,
			})
		}
	} else {
		log.WithField("collectID", info.Id).Infof("No collect dir has birth time support")

		err := c.collectFileStateHandler.UpdateCollectDirs(info.Cut.WhiteList, modConfig)
		if err != nil {
			log.WithField("collectID", info.Id).Errorf("file state handler update collect dirs: %v", err)
			return
		}
		// Get local files
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
		uploadFileStates = c.collectFileStateHandler.Files(
			file_state_handler.FilterIsCollecting(),
			file_state_handler.FilterTime(info.Cut.Start, info.Cut.End),
			uploadWhiteListFilter,
		)
	}

	// Get slave files if master-slave is enabled
	var slaveFiles []master.SlaveFileInfo
	if c.slaveRegistry != nil && c.masterClient != nil && c.masterConfig != nil {
		ctx, cancel := context.WithTimeout(context.Background(), c.masterConfig.RequestTimeout)
		defer cancel()

		taskReq := &master.TaskRequest{
			TaskID:              info.Id,
			StartTime:           info.Cut.Start,
			EndTime:             info.Cut.End,
			ScanFolders:         modConfig.CollectDirs,
			AdditionalFiles:     info.Cut.ExtraFiles,
			WhiteList:           info.Cut.WhiteList,
			RecursivelyWalkDirs: modConfig.RecursivelyWalkDirs,
		}

		responses := c.masterClient.RequestAllSlaveFilesByContent(ctx, c.slaveRegistry, taskReq)
		for slaveID, response := range responses {
			if response != nil && response.Success {
				log.WithField("collectID", info.Id).Infof("Slave %s returned %d files for rule collection", slaveID, len(response.Files))
				slaveFiles = append(slaveFiles, response.Files...)
			}
		}
		log.WithField("collectID", info.Id).Infof("Total slave files collected for rule: %d", len(slaveFiles))
	}

	for _, extraFileRaw := range info.Cut.ExtraFiles {
		extraFileAbs, err := filepath.Abs(extraFileRaw)
		if err != nil {
			log.WithField("collectID", info.Id).Errorf("AdditionalFiles get abs path for extra file: %v", err)
			continue
		}

		if !utils.CheckReadPath(extraFileAbs) {
			log.WithField("collectID", info.Id).Warnf("AdditionalFiles Path %s is not readable, skip!", extraFileAbs)
			continue
		}

		realPath, fileInfo, err := utils.GetRealFileInfo(extraFileAbs)
		if err != nil {
			log.WithField("collectID", info.Id).Errorf("AdditionalFiles get real file info for extra file: %v", err)
			continue
		}

		uploadFileStates = append(uploadFileStates, file_state_handler.FileState{
			Size:     fileInfo.Size(),
			IsDir:    fileInfo.IsDir(),
			Pathname: realPath,
		})
	}

	// Merge local and slave files
	localFiles := upload.ComputeRuleFileInfos(uploadFileStates)
	allFiles := make(map[string]model.FileInfo)

	// Add local files
	for p, fileInfo := range localFiles {
		allFiles[p] = fileInfo
		log.WithField("collectID", info.Id).Infof("Collecting local file: %s for startTime %d, endTime: %d", p, info.Cut.Start, info.Cut.End)
	}

	// Add slave files
	for _, slaveFileInfo := range slaveFiles {
		remotePath := slaveFileInfo.GetRemotePath()
		if remotePath == "" {
			log.WithField("collectID", info.Id).Warnf("slave file has empty remote path: %s, skip!", slaveFileInfo.Path)
			continue
		}
		// use slave file path as key to avoid duplication
		slaveFileInfo.FileInfo.Path = remotePath
		allFiles[remotePath] = slaveFileInfo.FileInfo

		log.WithField("collectID", info.Id).Infof("Collecting slave file: %s for startTime %d, endTime: %d", remotePath, info.Cut.Start, info.Cut.End)
	}

	log.WithField("collectID", info.Id).Infof("finish collecting for files start: %v, end: %v, total files: %d (local: %d, slave: %d)",
		info.Cut.Start, info.Cut.End, len(allFiles), len(localFiles), len(slaveFiles))

	rc := model.RecordCache{
		ProjectName:   info.ProjectName,
		Record:        info.Record,
		Labels:        info.Labels,
		Timestamp:     time.Now().UnixMilli(),
		DiagnosisTask: info.DiagnosisTask,
		OriginalFiles: allFiles,
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

		// Get title and description from record if not set in moment
		// displayname: moment.Title->recordTitle
		// description: moment.Description->recordTitle
		displayName := utils.GetStringOrDefault(recordTitle, moment.Title)
		description := utils.GetStringOrDefault(recordTitle, moment.Description)

		momentToCreate := model.Moment{
			Title:       displayName,
			Description: description,
			Timestamp:   ts,
			Duration:    duration,
			Code:        moment.Code,
			RuleName:    ruleName,
			Metadata:    moment.CustomFields,
		}

		if moment.CreateTask {
			momentToCreate.Task = model.Task{
				ShouldCreate: true,
				Title:        displayName,
				Description:  description,
				Assignee:     moment.AssignTo,
				SyncTask:     moment.SyncTask,
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

// EnhanceRuleHandlerWithMasterSlave adds master-slave support to rule handler.
func (c *CustomRuleHandler) EnhanceRuleHandlerWithMasterSlave(
	registry *master.SlaveRegistry,
	masterConfig *config.MasterConfig,
) {
	if registry == nil || masterConfig == nil {
		log.Warn("Master-slave components not provided, rule handler will work in normal mode")
		return
	}

	c.slaveRegistry = registry
	c.masterClient = master.NewClient(masterConfig)
	c.masterConfig = masterConfig

	log.Info("Rule handler enhanced with master-slave support")
}
