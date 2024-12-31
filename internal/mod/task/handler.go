package task

import (
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"encoding/json"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/collector"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CustomTaskHandler struct {
	reqClient api.RequestClient
	config    config.AppConfig
	storage   *storage.Storage
}

func NewTaskHandler(reqClient api.RequestClient, config config.AppConfig, storage *storage.Storage) mod.CustomHandler {
	return &CustomTaskHandler{
		reqClient: reqClient,
		config:    config,
		storage:   storage,
	}
}

func (c CustomTaskHandler) Run() error {
	deviceInfo := c.getDeviceInfo()
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		log.Info("Device info is not found, skipping task")
		return nil
	}

	cancellingTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_CANCELLING.Enum())
	if err != nil {
		log.Errorf("Failed to list device cancelling tasks: %v", err)
		return err
	}
	c.handleCancellingTasks(cancellingTasks)

	pendingTasks, err := c.reqClient.ListDeviceTasks(deviceInfo.GetName(), enums.TaskStateEnum_PENDING.Enum())
	if err != nil {
		log.Errorf("Failed to list device tasks: %v", err)
		return err
	}
	c.handlePendingTasks(pendingTasks)
	log.Infof("Task handler completed")
	return nil
}

func (c CustomTaskHandler) handleCancellingTasks(tasks []*openDpsV1alpha1Resource.Task) {
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

func (c CustomTaskHandler) handlePendingTasks(tasks []*openDpsV1alpha1Resource.Task) {
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

func (c CustomTaskHandler) handleUploadTask(task *openDpsV1alpha1Resource.Task) {
	log.Infof("Handling upload task %s", task.GetName())

	taskDetail := task.GetUploadTaskDetail()
	if taskDetail == nil {
		return
	}

	startTime := taskDetail.GetStartTime()
	endTime := taskDetail.GetEndTime()
	taskFolders := taskDetail.GetScanFolders()

	if len(taskFolders) == 0 {
		return
	}

	files := make(map[string]model.FileInfo)
	for _, folder := range taskFolders {
		canRead := utils.CheckReadPath(folder)
		if !canRead {
			log.Warnf("Path %s is not readable, skip!", folder)
			continue
		}

		err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Errorf("Failed to walk through folder %s", folder)
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

	if len(files) == 0 {
		_, err := c.reqClient.UpdateTaskState(task.GetName(), enums.TaskStateEnum_SUCCEEDED.Enum())
		if err != nil {
			log.Errorf("Failed to update task state %s: %v", task.GetName(), err)
		}
		return
	}

	projectName := strings.Split(task.GetName(), "/tasks/")[0]
	rc := model.RecordCache{
		ProjectName: projectName,
		Timestamp:   time.Now().UnixMilli(),
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

	_, err = c.reqClient.AddTaskTags(task.GetName(), tags)
	if err != nil {
		log.Errorf("Failed to add task tags: %v", err)
	}
}

func (c CustomTaskHandler) getDeviceInfo() *openDpsV1alpha1Resource.Device {
	bytes, err := (*c.storage).Get([]byte(constant.DeviceMetadataBucket), []byte(constant.DeviceInfoKey))
	if err != nil {
		return &openDpsV1alpha1Resource.Device{}
	}

	device := openDpsV1alpha1Resource.Device{}
	err = protojson.Unmarshal(bytes, &device)
	if err != nil {
		return &openDpsV1alpha1Resource.Device{}
	}

	return &device
}
