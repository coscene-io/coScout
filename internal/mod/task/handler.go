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
	cancellingTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_CANCELLING.Enum())
	if err != nil {
		log.Errorf("Failed to list device cancelling tasks: %v", err)
		c.errChan <- err
		return
	}
	//nolint: contextcheck // context is checked in the parent goroutine
	c.handleCancellingTasks(cancellingTasks)

	//nolint: contextcheck // context is checked in the parent goroutine
	pendingTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_PENDING.Enum())
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

		if cache.UploadTask == nil {
			continue
		}
		taskName, ok := cache.UploadTask["name"].(string)
		if !ok || taskName == "" {
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

	if len(taskFolders) == 0 {
		return
	}

	files := make(map[string]model.FileInfo)
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
			files[folder] = model.FileInfo{
				FileName: filepath.Base(folder),
				Size:     info.Size(),
				Path:     folder,
			}
			continue
		}

		err = filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				return nil
			}

			if err != nil {
				log.Errorf("Failed to walk through folder %s", folder)
				//nolint: nilerr // skip file
				return nil
			}

			if info.IsDir() {
				return nil
			}

			if info.ModTime().After(startTime.AsTime()) && info.ModTime().Before(endTime.AsTime()) {
				filename, err := filepath.Rel(folder, path)
				if err != nil {
					log.Errorf("Failed to get relative path: %v", err)
					filename = filepath.Base(path)
				}

				files[path] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     path,
				}
			}
			return nil
		})

		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", folder, err)
			continue
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
			files[file] = model.FileInfo{
				FileName: filepath.Base(file),
				Size:     info.Size(),
				Path:     file,
			}
			continue
		}

		err = filepath.Walk(file, func(path string, info os.FileInfo, err error) error {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				return nil
			}

			if err != nil {
				log.Errorf("Failed to walk through folder %s", path)
				//nolint: nilerr // skip file
				return nil
			}

			if info.IsDir() {
				return nil
			}

			filename, err := filepath.Rel(file, path)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(path)
			}

			files[path] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     path,
			}
			return nil
		})

		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", file, err)
			continue
		}
	}

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
			"name":  task.GetName(),
			"title": task.GetTitle(),
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
