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

package task

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/ThreeDotsLabs/watermill"
	gcmessage "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/collector"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type CustomTaskHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
	pubSub      *gochannel.GoChannel

	// Master-slave components (optional)
	slaveRegistry *master.SlaveRegistry
	masterClient  *master.Client
	masterConfig  *config.MasterConfig
}

func NewTaskHandler(reqClient api.RequestClient, confManager config.ConfManager, pubSub *gochannel.GoChannel, errChan chan error) *CustomTaskHandler {
	return &CustomTaskHandler{
		reqClient:   reqClient,
		confManager: confManager,
		errChan:     errChan,
		pubSub:      pubSub,
	}
}

func (c *CustomTaskHandler) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				ticker.Reset(config.TaskCheckInterval)

				log.Infof("Starting task handler")
				c.run(ctx)
			case <-ctx.Done():
				return
			}
		}
	}(ticker)

	<-ctx.Done()
	log.Infof("Task handler stopped")
}

func (c *CustomTaskHandler) run(_ context.Context) {
	deviceInfo := core.GetDeviceInfo(c.confManager.GetStorage())
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		log.Info("Device info is not found, skipping task")
		return
	}

	//nolint: contextcheck // context is checked in the parent goroutine
	cancellingUploadTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_CANCELLING.Enum(), enums.TaskCategoryEnum_UPLOAD.String())
	if err != nil {
		log.Errorf("Failed to list device cancelling upload tasks: %v", err)
		c.errChan <- err
		return
	}
	//nolint: contextcheck // context is checked in the parent goroutine
	c.handleCancellingTasks(cancellingUploadTasks)

	//nolint: contextcheck // context is checked in the parent goroutine
	cancellingDiagnosisTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_CANCELLING.Enum(), enums.TaskCategoryEnum_DIAGNOSIS.String())
	if err != nil {
		log.Errorf("Failed to list device cancelling diagnosis tasks: %v", err)
		c.errChan <- err
		return
	}
	//nolint: contextcheck // context is checked in the parent goroutine
	c.handleCancellingTasks(cancellingDiagnosisTasks)

	//nolint: contextcheck // context is checked in the parent goroutine
	pendingTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_PENDING.Enum(), enums.TaskCategoryEnum_UPLOAD.String())
	if err != nil {
		log.Errorf("Failed to list device tasks: %v", err)
		c.errChan <- err
		return
	}

	//nolint: contextcheck // context is checked in the parent goroutine
	c.handlePendingTasks(pendingTasks)
	log.Infof("Task handler completed")
}

func (c *CustomTaskHandler) handleCancellingTasks(tasks []*openDpsV1alpha1Resource.Task) {
	if len(tasks) == 0 {
		log.Infof("No cancelling tasks found")
		return
	}

	recordCaches := collector.FindAllRecordCaches()
	for _, rc := range recordCaches {
		canRead := utils.CheckReadPath(rc)
		if !canRead {
			log.Errorf("Record cache %s is not readable", rc)
			continue
		}

		bytes, err := os.ReadFile(rc)
		if err != nil {
			log.Errorf("Failed to read record cache %s: %v", rc, err)
			continue
		}

		cache := model.RecordCache{}
		err = json.Unmarshal(bytes, &cache)
		if err != nil {
			log.Errorf("Failed to unmarshal record cache %s: %v", rc, err)
			continue
		}

		taskName := ""
		if cache.UploadTask != nil {
			uploadTaskName, ok := cache.UploadTask["name"].(string)
			if ok && uploadTaskName != "" {
				taskName = uploadTaskName
			}
		}

		if cache.DiagnosisTask != nil {
			diagnosisTaskName, ok := cache.DiagnosisTask["name"].(string)
			if ok && diagnosisTaskName != "" {
				taskName = diagnosisTaskName
			}
		}

		if taskName == "" {
			log.Warnf("No task name found in record cache %s, skipping", rc)
			continue
		}

		for _, task := range tasks {
			if task.GetName() != taskName {
				continue
			}
			log.Infof("Cancelling task %s", taskName)

			cache.Skipped = true
			err = cache.Save()
			if err != nil {
				log.Errorf("Failed to save record cache %s: %v", rc, err)
				continue
			}
		}
	}

	for _, task := range tasks {
		if task.GetName() == "" {
			continue
		}

		log.Infof("Starting handle cancelling task %s", task.GetName())
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_CANCELLED.Enum())
		if err != nil {
			log.Errorf("Failed to update task state %s: %v", task.GetName(), err)
		}
	}
}

func (c *CustomTaskHandler) handlePendingTasks(tasks []*openDpsV1alpha1Resource.Task) {
	if len(tasks) == 0 {
		return
	}

	log.Infof("Found %d pending upload tasks", len(tasks))
	for _, task := range tasks {
		if task.GetName() == "" {
			continue
		}

		log.WithField("taskName", task.GetName()).Infof("Starting handle upload task")
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_PROCESSING.Enum())
		if err != nil {
			log.WithField("taskName", task.GetName()).Errorf("Failed to update task state: %v", err)
			continue
		}

		if task.GetUploadTaskDetail() != nil {
			c.handleUploadTask(task)
		} else {
			log.WithField("taskName", task.GetName()).Warnf("Upload task detail is nil, skip!")
		}

		log.WithField("taskName", task.GetName()).Infof("Finished handle upload task")
	}
}

func (c *CustomTaskHandler) handleUploadTask(task *openDpsV1alpha1Resource.Task) {
	log.WithField("taskName", task.GetName()).Infof("Handling upload task")

	taskDetail := task.GetUploadTaskDetail()
	if taskDetail == nil {
		return
	}

	startTime := taskDetail.GetStartTime()
	endTime := taskDetail.GetEndTime()
	taskFolders := taskDetail.GetScanFolders()
	additionalFiles := taskDetail.GetAdditionalFiles()

	log.WithField("taskName", task.GetName()).
		Infof("UploadTask start time: %s, end time: %s, folders: %v, additional files: %v",
			startTime.AsTime().String(), endTime.AsTime().String(), taskFolders, additionalFiles)

	// Get local files
	localFiles, noPermissionFolders := upload.ComputeUploadFiles(task.GetName(), taskFolders, additionalFiles, []string{}, true, startTime.AsTime().Unix(), endTime.AsTime().Unix())

	// Get slave files if master-slave is enabled
	allFiles := make(map[string]model.FileInfo)
	for path, fileInfo := range localFiles {
		allFiles[path] = fileInfo
	}

	var slaveFileCount int
	if c.slaveRegistry != nil && c.masterClient != nil && c.masterConfig != nil {
		ctx, cancel := context.WithTimeout(context.Background(), c.masterConfig.RequestTimeout)
		defer cancel()

		taskReq := &master.TaskRequest{
			TaskID:          task.GetName(),
			StartTime:       startTime.AsTime().Unix(),
			EndTime:         endTime.AsTime().Unix(),
			ScanFolders:     taskFolders,
			AdditionalFiles: additionalFiles,
		}

		responses := c.masterClient.RequestAllSlaveFiles(ctx, c.slaveRegistry, taskReq)
		for slaveID, response := range responses {
			if response != nil && response.Success {
				log.WithField("taskName", task.GetName()).Infof("Slave %s returned %d files", slaveID, len(response.Files))
				for _, file := range response.Files {
					remotePath := file.GetRemotePath()
					if remotePath == "" {
						continue
					}
					// use slave file path as key to avoid duplication
					file.FileInfo.Path = remotePath
					allFiles[remotePath] = file.FileInfo
					slaveFileCount++
				}
			}
		}
	}
	log.WithField("taskName", task.GetName()).Infof("Total files: %d (local: %d, slave: %d)", len(allFiles), len(localFiles), slaveFileCount)
	if len(allFiles) == 0 {
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_SUCCEEDED.Enum())
		if err != nil {
			log.WithField("taskName", task.GetName()).Errorf("Failed to update task state: %v", err)
		}

		if len(noPermissionFolders) > 0 {
			tags := make(map[string]string)
			tags["noPermissionFiles"] = strings.Join(noPermissionFolders, ",")
			_, err = c.reqClient.AddTaskTags(task.GetName(), tags)
			if err != nil {
				log.Errorf("Failed to add task tags: %v", err)
			}

			log.WithField("taskName", task.GetName()).Warnf("UploadTask has %d no permission files", len(noPermissionFolders))
		}

		return
	}

	projectName := strings.Split(task.GetName(), "/tasks/")[0]
	rc := model.RecordCache{
		ProjectName: projectName,
		Timestamp:   time.Now().UnixMilli(),
		Labels:      taskDetail.GetLabels(),
		UploadTask: map[string]interface{}{
			"name":        task.GetName(),
			"title":       task.GetTitle(),
			"description": task.GetDescription(),
		},
		OriginalFiles: allFiles,
	}
	err := rc.Save()
	if err != nil {
		log.Errorf("Failed to save record cache: %v", err)
		return
	}

	log.WithField("taskName", task.GetName()).Infof("Record cache saved for task")
	tags := make(map[string]string)
	tags["totalFiles"] = strconv.Itoa(len(allFiles))
	if len(noPermissionFolders) > 0 {
		tags["noPermissionFiles"] = strings.Join(noPermissionFolders, ",")

		log.WithField("taskName", task.GetName()).Warnf("UploadTask has %d no permission files", len(noPermissionFolders))
	}

	_, err = c.reqClient.AddTaskTags(task.GetName(), tags)
	if err != nil {
		log.WithField("taskName", task.GetName()).Errorf("Failed to add task tags: %v", err)
	}

	msg := gcmessage.NewMessage(watermill.NewUUID(), []byte(task.GetName()))
	err = c.pubSub.Publish(constant.TopicCollectMsg, msg)
	if err != nil {
		log.Errorf("Failed to publish collect message: %v", err)
	}
}

// EnhanceTaskHandlerWithMasterSlave adds master-slave support to task handler.
func (c *CustomTaskHandler) EnhanceTaskHandlerWithMasterSlave(
	registry *master.SlaveRegistry,
	masterConfig *config.MasterConfig,
) {
	if registry == nil || masterConfig == nil {
		log.Warn("Master-slave components not provided, task handler will work in normal mode")
		return
	}

	c.slaveRegistry = registry
	c.masterClient = master.NewClient(masterConfig)
	c.masterConfig = masterConfig

	log.Info("Task handler enhanced with master-slave support")
}
