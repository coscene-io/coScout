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
	"path/filepath"
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
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/djherbis/times"
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

		log.Infof("Starting handle upload task %s", task.GetName())
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_PROCESSING.Enum())
		if err != nil {
			log.Errorf("Failed to update task state %s: %v", task.GetName(), err)
			continue
		}

		if task.GetUploadTaskDetail() != nil {
			c.handleUploadTask(task)
		}
	}
}

func (c *CustomTaskHandler) handleUploadTask(task *openDpsV1alpha1Resource.Task) {
	log.Infof("Handling upload task %s", task.GetName())

	taskDetail := task.GetUploadTaskDetail()
	if taskDetail == nil {
		return
	}

	startTime := taskDetail.GetStartTime()
	endTime := taskDetail.GetEndTime()
	taskFolders := taskDetail.GetScanFolders()
	additionalFiles := taskDetail.GetAdditionalFiles()

	log.Infof("UploadTask %s, start time: %s, end time: %s, folders: %v, additional files: %v",
		task.GetName(), startTime.AsTime().String(), endTime.AsTime().String(), taskFolders, additionalFiles)

	// Scan local files
	localFiles := make(map[string]model.FileInfo)
	noPermissionFolders := make([]string, 0)
	for _, folder := range taskFolders {
		canRead := utils.CheckReadPath(folder)
		if !canRead {
			log.Warnf("Path %s is not readable, skip!", folder)

			noPermissionFolders = append(noPermissionFolders, folder)
			continue
		}

		info, err := os.Stat(folder)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}
		if !info.IsDir() {
			localFiles[folder] = model.FileInfo{
				FileName: filepath.Base(folder),
				Size:     info.Size(),
				Path:     folder,
			}
			continue
		}

		filePaths, err := utils.GetAllFilePaths(folder, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to get all file paths in folder %s: %v", folder, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			info, err := os.Stat(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			log.Infof("file %s, mod time: %s", path, info.ModTime().String())
			//nolint: nestif // check file modification time
			if info.ModTime().After(startTime.AsTime()) && info.ModTime().Before(endTime.AsTime()) {
				filename, err := filepath.Rel(folder, path)
				if err != nil {
					log.Errorf("Failed to get relative path: %v", err)
					filename = filepath.Base(path)
				}

				localFiles[path] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     path,
				}
			} else {
				stat, err := times.Stat(path)
				if err != nil {
					log.Errorf("Failed to get file times for %s: %v", path, err)
					continue
				}

				if stat.HasBirthTime() {
					log.Infof("File %s has birth time: %s", path, stat.BirthTime().String())

					if stat.BirthTime().After(startTime.AsTime()) && stat.BirthTime().Before(endTime.AsTime()) {
						filename, err := filepath.Rel(folder, path)
						if err != nil {
							log.Errorf("Failed to get relative path: %v", err)
							filename = filepath.Base(path)
						}

						localFiles[path] = model.FileInfo{
							FileName: filename,
							Size:     info.Size(),
							Path:     path,
						}
					}
				}
			}
		}
	}

	for _, file := range additionalFiles {
		canRead := utils.CheckReadPath(file)
		if !canRead {
			log.Warnf("Path %s is not readable, skip!", file)

			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		info, err := os.Stat(file)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}
		if !info.IsDir() {
			localFiles[file] = model.FileInfo{
				FileName: filepath.Base(file),
				Size:     info.Size(),
				Path:     file,
			}
			continue
		}

		// Clean the file path to handle trailing slashes correctly
		cleanFile := filepath.Clean(file)
		parentFolder := filepath.Dir(cleanFile)

		filePaths, err := utils.GetAllFilePaths(file, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", file, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			info, err := os.Stat(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			filename, err := filepath.Rel(parentFolder, path)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(path)
			}

			localFiles[path] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     path,
			}
		}
	}

	// Get slave files if master-slave is enabled
	var slaveFiles []master.SlaveFileInfo
	if c.slaveRegistry != nil && c.masterClient != nil && c.masterConfig != nil {
		ctx, cancel := context.WithTimeout(context.Background(), c.masterConfig.RequestTimeout)
		defer cancel()

		taskReq := &master.TaskRequest{
			TaskID:          task.GetName(),
			StartTime:       startTime.AsTime(),
			EndTime:         endTime.AsTime(),
			ScanFolders:     taskFolders,
			AdditionalFiles: additionalFiles,
		}

		responses := c.masterClient.RequestAllSlaveFiles(ctx, c.slaveRegistry, taskReq)
		for slaveID, response := range responses {
			if response != nil && response.Success {
				log.Infof("Slave %s returned %d files for task %s", slaveID, len(response.Files), task.GetName())
				slaveFiles = append(slaveFiles, response.Files...)
			}
		}
		log.Infof("Total slave files collected for task: %d", len(slaveFiles))
	}

	// Merge local and slave files
	allFiles := make(map[string]model.FileInfo)
	
	// Add local files
	for path, fileInfo := range localFiles {
		allFiles[path] = fileInfo
	}
	
	// Add slave files
	for _, slaveFileInfo := range slaveFiles {
		remotePath := slaveFileInfo.GetRemotePath()
		allFiles[remotePath] = slaveFileInfo.FileInfo
	}

	log.Infof("Upload task scan completed - local files: %d, slave files: %d, total: %d", 
		len(localFiles), len(slaveFiles), len(allFiles))

	if len(allFiles) == 0 {
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_SUCCEEDED.Enum())
		if err != nil {
			log.Errorf("Failed to update task state %s: %v", task.GetName(), err)
		}

		if len(noPermissionFolders) > 0 {
			tags := make(map[string]string)
			tags["noPermissionFiles"] = strings.Join(noPermissionFolders, ",")
			_, err = c.reqClient.AddTaskTags(task.GetName(), tags)
			if err != nil {
				log.Errorf("Failed to add task tags: %v", err)
			}
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

	log.Infof("Record cache saved for task %s", task.GetName())
	tags := make(map[string]string)
	tags["totalFiles"] = strconv.Itoa(len(allFiles))
	if len(noPermissionFolders) > 0 {
		tags["noPermissionFiles"] = strings.Join(noPermissionFolders, ",")
	}

	_, err = c.reqClient.AddTaskTags(task.GetName(), tags)
	if err != nil {
		log.Errorf("Failed to add task tags: %v", err)
	}

	msg := gcmessage.NewMessage(watermill.NewUUID(), []byte(task.GetName()))
	err = c.pubSub.Publish(constant.TopicCollectMsg, msg)
	if err != nil {
		log.Errorf("Failed to publish collect message: %v", err)
	}
}

// EnhanceTaskHandlerWithMasterSlave adds master-slave support to task handler
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
