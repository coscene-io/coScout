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
	stderrors "errors"
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

type ruleItemEnvelope struct {
	item   rule_engine.RuleItem
	result chan error
}

type CustomRuleHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
	pubSub      *gochannel.GoChannel

	listenFileStateHandler  file_state_handler.FileStateHandler
	collectFileStateHandler file_state_handler.FileStateHandler
	listenChan              chan string
	ruleItemChan            chan ruleItemEnvelope
	engine                  Engine
	inFlightMu              sync.Mutex
	inFlightFiles           map[string]struct{}

	// Master-slave components (optional)
	slaveRegistry *master.SlaveRegistry
	masterClient  ruleSlaveFileRequester
	masterConfig  *config.MasterConfig

	cleanCollectInfo func(model.CollectInfo) string
}

type ruleSlaveFileRequester interface {
	RequestAllSlaveFilesByContent(context.Context, *master.SlaveRegistry, *master.TaskRequest) map[string]*master.TaskResponse
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
		ruleItemChan:            make(chan ruleItemEnvelope, 1000),
		engine:                  Engine{reqClient: reqClient, ruleDebounceTime: make(map[string]*time.Time)},
		inFlightFiles:           make(map[string]struct{}),
		pubSub:                  pubSub,
		cleanCollectInfo: func(info model.CollectInfo) string {
			return info.Clean()
		},
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

				appConfig, err := c.confManager.LoadWithRemote()
				if err != nil {
					if appConfig == nil {
						log.Errorf("load rule config: %v", err)
						continue
					}
					log.Warnf("Unable to reload rule config, using last-known-good config: %v", err)
				}
				confConfig, ok := appConfig.Mod.Config.(config.DefaultModConfConfig)
				if ok {
					*modConfig = confConfig
				}

				device := core.GetDeviceInfo(c.confManager.GetStorage())
				if device == nil {
					log.Errorf("get device info failed")
					continue
				}
				c.engine.SetDeviceName(device.GetName())

				//nolint: contextcheck// context is checked in the parent goroutine
				apiRules, err := c.reqClient.ListDeviceDiagnosisRules(device.GetName())
				if err != nil {
					log.Errorf("list device diagnosis rules: %v", err)
					select {
					case c.errChan <- errors.Errorf("list device diagnosis rules: %v", err):
					case <-ctx.Done():
						return
					}
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
	select {
	case <-modFirstUpdated:
	case <-ctx.Done():
		log.Infof("Rule handler stopped before first rule update")
		return
	}

	// start a periodic goroutine to search for files to be processed and send to listenChan
	listenTicker := time.NewTicker(1 * time.Second)
	defer listenTicker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				listenTicker.Reset(config.RuleCheckListenFilesInterval)

				c.sendFilesToBeProcessed(ctx, modConfig)
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
			case envelope := <-c.ruleItemChan:
				consumeErr := c.engine.ConsumeNext(envelope.item)
				if envelope.result != nil {
					envelope.result <- consumeErr
				}
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

				c.scanCollectInfosAndHandle(ctx, modConfig)
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
		select {
		case c.errChan <- errors.Wrap(err, "subscribe to rule message"):
		case <-ctx.Done():
		}
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

			select {
			case c.ruleItemChan <- ruleItemEnvelope{item: item}:
				msg.Ack()
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *CustomRuleHandler) sendFilesToBeProcessed(
	ctx context.Context,
	modConfig *config.DefaultModConfConfig,
) {
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
		if !c.addFileInFlight(fileState.Pathname) {
			continue
		}

		select {
		case c.listenChan <- fileState.Pathname:
		case <-ctx.Done():
			c.releaseFileInFlight(fileState.Pathname)
			return
		}
	}
}

func (c *CustomRuleHandler) addFileInFlight(filename string) bool {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()

	if c.inFlightFiles == nil {
		c.inFlightFiles = make(map[string]struct{})
	}
	if _, exists := c.inFlightFiles[filename]; exists {
		return false
	}
	c.inFlightFiles[filename] = struct{}{}
	return true
}

func (c *CustomRuleHandler) releaseFileInFlight(filename string) {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	delete(c.inFlightFiles, filename)
}

func (c *CustomRuleHandler) isFileInFlight(filename string) bool {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	_, exists := c.inFlightFiles[filename]
	return exists
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

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				c.releaseFileInFlight(fileToProcess)
				wg.Wait()
				return
			}

			wg.Add(1)
			go func(filename string) {
				defer wg.Done()
				defer func() { <-semaphore }() // release the semaphore
				defer c.releaseFileInFlight(filename)

				if err := c.processFileWithRule(ctx, filename); err != nil {
					if !shouldMarkFileFailed(ctx, err) {
						return
					}
					log.Errorf("process file %s with rule: %v", filename, err)
					if markErr := c.listenFileStateHandler.MarkFailedFile(filename); markErr != nil {
						log.Errorf("mark failed file %s: %v", filename, markErr)
					}
					return
				}

				if err := c.listenFileStateHandler.MarkProcessedFile(filename); err != nil {
					log.Errorf("mark processed file: %v", err)
					return
				}
				log.Infof("Finished processing file: %v", filename)
			}(fileToProcess)
		case <-ctx.Done():
			wg.Wait()
			return
		}
	}
}

func shouldMarkFileFailed(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

func (c *CustomRuleHandler) processFileWithRule(
	ctx context.Context,
	filename string,
) error {
	log.Infof("RuleEngine exec file: %v", filename)
	handler := c.listenFileStateHandler.GetFileHandler(filename)
	if handler == nil {
		// this should not happen
		return errors.Errorf("get file handler failed for file: %v", filename)
	}

	localRuleItems := make(chan rule_engine.RuleItem)
	handlerDone := make(chan error, 1)
	go func() {
		handlerDone <- handler.SendRuleItems(
			ctx,
			filename,
			c.engine.activeTopicSet(),
			localRuleItems,
		)
	}()

	var consumeErr error
	for {
		select {
		case item := <-localRuleItems:
			result := make(chan error, 1)
			select {
			case c.ruleItemChan <- ruleItemEnvelope{
				item:   item,
				result: result,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}

			select {
			case itemConsumeErr := <-result:
				if itemConsumeErr != nil {
					consumeErr = stderrors.Join(
						consumeErr,
						errors.Wrap(itemConsumeErr, "consume rule item"),
					)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		case handlerErr := <-handlerDone:
			return stderrors.Join(handlerErr, consumeErr)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// scanCollectInfosAndHandle handles all collect info files within the collect info dir.
func (c *CustomRuleHandler) scanCollectInfosAndHandle(
	ctx context.Context,
	modConfig *config.DefaultModConfConfig,
) {
	log.Infof("Starts to scan collect info dir")

	// Search for files under the collect info dir and handles them
	collectInfoDir := config.GetCollectInfoFolder()
	entries, err := os.ReadDir(collectInfoDir)
	if err != nil {
		log.Errorf("read collect info dir: %v", err)
		select {
		case c.errChan <- errors.Wrap(err, "read collect info dir"):
		case <-ctx.Done():
		}
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
		if ctx.Err() != nil {
			return
		}

		collectInfo := &model.CollectInfo{}
		if err := collectInfo.Load(collectInfoId); err != nil {
			log.Errorf("load collect info: %v", err)
			select {
			case c.errChan <- errors.Wrap(err, "load collect info"):
			case <-ctx.Done():
				return
			}
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

		log.WithField("collectID", collectInfo.Id).Infof("Found collect info to handle")
		c.handleCollectInfo(ctx, *collectInfo, *modConfig)
	}
	log.Infof("Finished scanning collect info dir, found %d collect info files", len(collectInfoIds))
}

// handleCollectInfo handles a single the collect info.
func (c *CustomRuleHandler) handleCollectInfo(
	ctx context.Context,
	info model.CollectInfo,
	modConfig config.DefaultModConfConfig,
) {
	ruleName, _ := info.DiagnosisTask["rule_name"].(string)
	ruleDisplayName, _ := info.DiagnosisTask["rule_display_name"].(string)
	collectLog := log.WithFields(log.Fields{
		"collectID":       info.Id,
		"ruleName":        ruleName,
		"ruleDisplayName": ruleDisplayName,
		"project":         info.ProjectName,
	})

	if info.Skip {
		collectLog.Infof("skipping collect info, cleaning")
		c.clean(info)
		return
	}

	if info.Cut == nil {
		collectLog.Errorf("collect info cut is nil, cleaning")
		c.clean(info)
		return
	}

	if err := upload.ValidateTimeWindow(info.Cut.Start, info.Cut.End); err != nil {
		if errors.Is(err, upload.ErrTimeWindowNotReady) {
			collectLog.WithFields(log.Fields{
				"start": info.Cut.Start,
				"end":   info.Cut.End,
			}).Infof("collect info starts beyond the future tolerance and will be consumed as an empty successful scan: %v", err)
			if cleanedPath := c.clean(info); cleanedPath == "" {
				collectLog.Errorf("failed to clean future collect info after empty successful scan")
			}
			return
		}
		collectLog.WithFields(log.Fields{
			"start": info.Cut.Start,
			"end":   info.Cut.End,
		}).Errorf("collect info has a permanently invalid time window, cleaning: %v", err)
		if cleanedPath := c.clean(info); cleanedPath == "" {
			collectLog.Errorf("failed to clean collect info with permanently invalid time window")
		}
		return
	}

	collectLog.WithFields(log.Fields{
		"start": info.Cut.Start,
		"end":   info.Cut.End,
	}).Infof("collecting files for collect info")
	recordTitle, _ := info.Record["title"].(string)

	uploadFileStates := make([]file_state_handler.FileState, 0)
	hasBirthTime := utils.HasBirthTime(modConfig.CollectDirs)
	//nolint: nestif // nested if is more readable here.
	if hasBirthTime {
		collectLog.Infof("at least one collect dir has birth time support")

		uploadFiles, noPermissionFiles, err := upload.ComputeUploadFiles(info.Id, modConfig.CollectDirs, []string{}, info.Cut.WhiteList, modConfig.RecursivelyWalkDirs, info.Cut.Start, info.Cut.End)
		if err != nil {
			collectLog.Errorf("collect info scan did not run: %v", err)
			return
		}
		if len(noPermissionFiles) > 0 {
			collectLog.WithField("files", noPermissionFiles).Infof("found no permission files")
		}
		for _, file := range uploadFiles {
			uploadFileStates = append(uploadFileStates, file_state_handler.FileState{
				Size:     file.Size,
				Pathname: file.Path,
				IsDir:    false,
			})
		}
	} else {
		collectLog.Infof("no collect dir has birth time support")

		err := c.collectFileStateHandler.UpdateCollectDirs(info.Cut.WhiteList, modConfig)
		if err != nil {
			collectLog.Errorf("file state handler update collect dirs: %v", err)
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
		requestCtx, cancel := context.WithTimeout(ctx, c.masterConfig.RequestTimeout)
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

		responses := c.masterClient.RequestAllSlaveFilesByContent(requestCtx, c.slaveRegistry, taskReq)
		switch master.TaskResponsesTimeWindowErrorCode(responses) {
		case master.TaskErrorCodeInvalidTimeWindow:
			collectLog.Errorf("a slave rejected the collect info time window as permanently invalid, cleaning")
			if cleanedPath := c.clean(info); cleanedPath == "" {
				collectLog.Errorf("failed to clean collect info rejected by slave")
			}
			return
		case master.TaskErrorCodeTimeWindowNotReady:
			collectLog.Infof("a legacy slave reported a not-ready time window; treating that response as empty and continuing with available results")
		}
		for slaveID, response := range successfulSlaveResponses(responses) {
			collectLog.WithFields(log.Fields{
				"slaveID": slaveID,
				"files":   len(response.Files),
			}).Infof("slave returned files for rule collection")
			slaveFiles = append(slaveFiles, response.Files...)
		}
		collectLog.WithField("slaveFiles", len(slaveFiles)).Infof("total slave files collected for rule")
	}

	for _, extraFileRaw := range info.Cut.ExtraFiles {
		extraFileAbs, err := filepath.Abs(extraFileRaw)
		if err != nil {
			collectLog.WithField("extraFile", extraFileRaw).Errorf("additional file get abs path failed: %v", err)
			continue
		}

		if !utils.CheckReadPath(extraFileAbs) {
			collectLog.WithField("extraFile", extraFileAbs).Warnf("additional file is not readable, skip")
			continue
		}

		realPath, fileInfo, err := utils.GetRealFileInfo(extraFileAbs)
		if err != nil {
			collectLog.WithField("extraFile", extraFileAbs).Errorf("additional file get real file info failed: %v", err)
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
		collectLog.WithFields(log.Fields{
			"file":  p,
			"start": info.Cut.Start,
			"end":   info.Cut.End,
		}).Infof("collecting local file")
	}

	// Add slave files
	for _, slaveFileInfo := range slaveFiles {
		remotePath := slaveFileInfo.GetRemotePath()
		if remotePath == "" {
			collectLog.WithField("file", slaveFileInfo.Path).Warnf("slave file has empty remote path, skip")
			continue
		}
		// use slave file path as key to avoid duplication
		slaveFileInfo.FileInfo.Path = remotePath
		allFiles[remotePath] = slaveFileInfo.FileInfo

		collectLog.WithFields(log.Fields{
			"file":  remotePath,
			"start": info.Cut.Start,
			"end":   info.Cut.End,
		}).Infof("collecting slave file")
	}

	collectLog.WithFields(log.Fields{
		"start":      info.Cut.Start,
		"end":        info.Cut.End,
		"totalFiles": len(allFiles),
		"localFiles": len(localFiles),
		"slaveFiles": len(slaveFiles),
	}).Infof("finished collecting files")

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
		collectLog.WithFields(log.Fields{
			"totalFiles": len(allFiles),
		}).Errorf("save record cache failed: %v", err)
		return
	}
	collectLog.WithFields(log.Fields{
		"recordCache": rc.GetRecordCachePath(),
		"totalFiles":  len(allFiles),
		"moments":     len(rc.Moments),
	}).Infof("saved record cache")

	if cleanId := c.clean(info); cleanId == "" {
		collectLog.WithField("recordCache", rc.GetRecordCachePath()).Errorf("clean collect info failed")
	} else {
		collectLog.WithFields(log.Fields{
			"recordCache": rc.GetRecordCachePath(),
			"cleaned":     cleanId,
		}).Infof("cleaned collect info")
	}

	msg := gcmessage.NewMessage(watermill.NewUUID(), []byte(rc.GetRecordCachePath()))
	err := c.pubSub.Publish(constant.TopicCollectMsg, msg)
	if err != nil {
		collectLog.WithField("recordCache", rc.GetRecordCachePath()).Errorf("failed to publish collect message: %v", err)
	} else {
		collectLog.WithField("recordCache", rc.GetRecordCachePath()).Infof("published collect message")
	}
}

func successfulSlaveResponses(responses map[string]*master.TaskResponse) map[string]*master.TaskResponse {
	successful := make(map[string]*master.TaskResponse)
	for slaveID, response := range responses {
		if response != nil && response.Success {
			successful[slaveID] = response
		}
	}
	return successful
}

func (c *CustomRuleHandler) clean(info model.CollectInfo) string {
	if c.cleanCollectInfo != nil {
		return c.cleanCollectInfo(info)
	}
	return info.Clean()
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
