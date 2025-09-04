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

	files, noPermissionFolders := computeUploadFiles(taskFolders, additionalFiles, startTime.AsTime(), endTime.AsTime())
	if len(files) == 0 {
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
		OriginalFiles: files,
	}
	err := rc.Save()
	if err != nil {
		log.Errorf("Failed to save record cache: %v", err)
		return
	}

	log.Infof("Record cache saved for task %s", task.GetName())
	tags := make(map[string]string)
	tags["totalFiles"] = strconv.Itoa(len(files))
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

func computeUploadFiles(scanFolders []string, additionalFiles []string, startTime time.Time, endTime time.Time) (map[string]model.FileInfo, []string) {
	files := make(map[string]model.FileInfo)
	noPermissionFolders := make([]string, 0)

	for _, folder := range scanFolders {
		if !utils.CheckReadPath(folder) {
			log.Warnf("Path %s is not readable, skip!", folder)

			noPermissionFolders = append(noPermissionFolders, folder)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(folder)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
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

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			log.Infof("file %s, mod time: %s", realPath, info.ModTime().String())
			//nolint: nestif // check file modification time
			if info.ModTime().After(startTime) && info.ModTime().Before(endTime) {
				filename, err := filepath.Rel(folder, realPath)
				if err != nil {
					log.Errorf("Failed to get relative path: %v", err)
					filename = filepath.Base(realPath)
				}

				files[realPath] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     realPath,
				}
			} else {
				stat, err := times.Stat(realPath)
				if err != nil {
					log.Errorf("Failed to get file times for %s: %v", realPath, err)
					continue
				}

				if stat.HasBirthTime() {
					log.Infof("File %s has birth time: %s", realPath, stat.BirthTime().String())
					if stat.BirthTime().After(startTime) && stat.BirthTime().Before(endTime) {
						filename, err := filepath.Rel(folder, realPath)
						if err != nil {
							log.Errorf("Failed to get relative path: %v", err)
							filename = filepath.Base(realPath)
						}

						files[realPath] = model.FileInfo{
							FileName: filename,
							Size:     info.Size(),
							Path:     realPath,
						}
					}
				}
			}
		}
	}

	for _, file := range additionalFiles {
		if !utils.CheckReadPath(file) {
			log.Warnf("Path %s is not readable, skip!", file)

			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(file)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		// Clean the file path to handle trailing slashes correctly
		cleanFile := filepath.Clean(realPath)
		parentFolder := filepath.Dir(cleanFile)
		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", realPath, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			filename, err := filepath.Rel(parentFolder, realPath)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			files[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
		}
	}

	return files, noPermissionFolders
}
