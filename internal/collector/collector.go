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

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	openAnaV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/enums"
	openAnaV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/resources"
	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/name"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

func FindAllRecordCaches() []string {
	baseFolder := config.GetRecordCacheFolder()
	var records []string
	if !utils.CheckReadPath(baseFolder) {
		return records
	}

	err := filepath.Walk(baseFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Errorf("walk through cache directory failed: %v", err)
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".cos/state.json") {
			records = append(records, path)
		}
		return nil
	})
	if err != nil {
		log.Errorf("walk through cache directory failed: %v", err)
	}
	return records
}

func Collect(ctx context.Context, reqClient *api.RequestClient, confManager *config.ConfManager, pubSub *gochannel.GoChannel, errorChan chan error) error {
	uploadChan := make(chan *model.RecordCache, 10)
	triggerChan := make(chan struct{}, 1)

	go Upload(ctx, reqClient, confManager, uploadChan, errorChan)
	ticker := time.NewTicker(config.CollectionInterval)
	defer ticker.Stop()

	go func(t *time.Ticker) {
		defer log.Warn("collector ticker stopped")
		for {
			select {
			case <-t.C:
				select {
				case triggerChan <- struct{}{}:
				default: // Skip if there's already a pending trigger
				}
			case <-ctx.Done():
				return
			}
		}
	}(ticker)

	go func() {
		defer log.Warn("collector goroutine stopped")
		for {
			select {
			case <-ctx.Done():
				return
			case <-triggerChan:
				appConfig := confManager.LoadWithRemote()
				getStorage := confManager.GetStorage()

				//nolint: contextcheck // context is checked in the parent goroutine
				err := handleRecordCaches(uploadChan, reqClient, appConfig, getStorage)
				if err != nil {
					errorChan <- err
				}

				time.Sleep(1 * time.Second)
			}
		}
	}()

	go triggerUpload(ctx, pubSub, triggerChan)

	<-ctx.Done()
	log.Warnf("collector context done")
	return nil
}

func triggerUpload(ctx context.Context, pubSub *gochannel.GoChannel, triggerChan chan struct{}) {
	messages, err := pubSub.Subscribe(ctx, constant.TopicCollectMsg)
	if err != nil {
		log.Errorf("subscribe to collect message failed: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-messages:
			if msg == nil {
				log.Warn("received nil collect message, skip")
				continue
			}

			log.Infof("Received collect message, start collecting for upload")
			msg.Ack()

			time.Sleep(1 * time.Second)

			select {
			case triggerChan <- struct{}{}:
			default: // Skip if there's already a pending trigger
			}
		}
	}
}

func handleRecordCaches(uploadChan chan *model.RecordCache, reqClient *api.RequestClient, config *config.AppConfig, storage *storage.Storage) error {
	log.Infof("Start collecting record caches")

	deviceInfo := core.GetDeviceInfo(storage)
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		log.Warn("device info not found, skip collecting")
		//nolint: err113 // we need to check if device info is found
		return errors.New("device info not found")
	}

	// get all record caches
	records := FindAllRecordCaches()
	if len(records) == 0 {
		log.Info("No record cache found, skip collecting")
		return nil
	}

	// upload all record caches
	for _, record := range records {
		log.Infof("Processing record cache: %s", record)

		if !utils.CheckReadPath(record) {
			log.Warnf("record cache %s not exist or has no permission", record)
			continue
		}

		data, err := os.ReadFile(record)
		if err != nil {
			log.Errorf("read record cache failed: %v", err)
			continue
		}

		rc := model.RecordCache{}
		if err := json.Unmarshal(data, &rc); err != nil {
			log.Errorf("unmarshal record cache failed: %v", err)
			continue
		}

		// check if record cache is expired
		if checkRecordCacheExpired(config.Collector.DeleteAfterIntervalInHours, rc.Timestamp, record) {
			log.Infof("Record cache %s is expired, delete it", record)
			continue
		}

		if rc.Uploaded || rc.Skipped {
			log.Infof("Record cache %s has been uploaded or skipped, skip it", record)
			continue
		}

		// create related resources
		createRelatedRecordResources(deviceInfo, &rc, reqClient, config.Device)

		if rcName, ok := rc.Record["name"].(string); ok && rcName != "" {
			uploadChan <- &rc
		}
	}
	log.Infof("Finish collecting record caches")
	return nil
}

func obtainEventFromMoment(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, moment *model.Moment, recordName, recordTitle string, reqClient *api.RequestClient) {
	if moment.Name == "" {
		// displayname: moment.Title->recordTitle
		// description: moment.Description->recordTitle
		displayName := utils.GetStringOrDefault(recordTitle, moment.Title)
		description := utils.GetStringOrDefault(recordTitle, moment.Description)

		sec, nanos := utils.NormalizeFloatTimestamp(moment.Timestamp)
		moment.Timestamp = float64(sec) + float64(nanos)/1e9

		event := openDpsV1alpha1Resource.Event{
			Record:           recordName,
			DisplayName:      displayName,
			Description:      description,
			CustomizedFields: moment.Metadata,
			TriggerTime: &timestamppb.Timestamp{
				Seconds: int64(moment.Timestamp),
				// timestamp is in seconds, so we need to convert nanoseconds to seconds
				Nanos: int32((moment.Timestamp - float64(int64(moment.Timestamp))) * 1e9),
			},
			Duration: &durationpb.Duration{
				Seconds: int64(moment.Duration),
				// duration is in seconds, so we need to convert nanoseconds to seconds
				Nanos: int32((moment.Duration - float64(int64(moment.Duration))) * 1e9),
			},
			Device: deviceInfo,
		}
		if moment.RuleName != "" {
			event.Rule = &openDpsV1alpha1Resource.DiagnosisRule{
				Name: moment.RuleName,
			}
		}
		obtainEvent, err := reqClient.ObtainEvent(rc.ProjectName, &event)
		if err != nil {
			log.Errorf("obtain event failed: %v", err)
		} else {
			moment.Name = obtainEvent.GetName()
			moment.IsNew = true
		}

		err = rc.Save()
		if err != nil {
			log.Errorf("save record cache failed: %v", err)
		}
	}
}

func triggerDeviceEventFromMoment(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, moment *model.Moment, recordName string, reqClient *api.RequestClient, deviceConfig *config.DeviceConfig) {
	if moment.Name != "" && moment.Event == nil {
		diagnosisRuleId := ""
		if diagnosisRuleName, err := name.NewDiagnosisRule(moment.RuleName); err == nil {
			diagnosisRuleId = diagnosisRuleName.Id
		}

		deviceEvent := openAnaV1alpha1Resource.DeviceEvent{
			Code:       moment.Code,
			Parameters: moment.Metadata,
			TriggerTime: &timestamppb.Timestamp{
				Seconds: int64(moment.Timestamp),
				Nanos:   int32((moment.Timestamp - float64(int64(moment.Timestamp))) * 1e9),
			},
			Duration: &durationpb.Duration{
				Seconds: int64(moment.Duration),
				Nanos:   int32((moment.Duration - float64(int64(moment.Duration))) * 1e9),
			},
			EventSource:     openAnaV1alpha1Enum.EventSourceEnum_DEVICE,
			Moment:          moment.Name,
			Device:          deviceInfo.GetName(),
			Record:          recordName,
			DiagnosisRuleId: diagnosisRuleId,
			DeviceContext:   getDeviceExtraInfos(deviceConfig.ExtraFiles),
		}

		err := reqClient.TriggerDeviceEvent(rc.ProjectName, &deviceEvent)
		if err != nil {
			log.Errorf("trigger device event failed: %v", err)
		}
	}
}

func upsertTaskFromMoment(rc *model.RecordCache, moment *model.Moment, recordTitle string, reqClient *api.RequestClient) {
	if !moment.Task.ShouldCreate {
		return
	}

	//nolint: nestif // we need to check if the task is new
	if moment.Name != "" && moment.Task.Name == "" {
		// displayname: moment.Task.Title->moment.Title->recordTitle
		// description: moment.Task.Description->moment.Description->recordTitle
		displayName := utils.GetStringOrDefault(recordTitle, moment.Task.Title, moment.Title)
		description := utils.GetStringOrDefault(recordTitle, moment.Task.Description, moment.Description)

		// Create new task
		task := &openDpsV1alpha1Resource.Task{
			Title:       displayName,
			Description: description,
			Assignee:    moment.Task.Assignee,
			Category:    openDpsV1alpha1Enum.TaskCategoryEnum_COMMON,
			State:       openDpsV1alpha1Enum.TaskStateEnum_PENDING,
			Detail: &openDpsV1alpha1Resource.Task_CommonTaskDetail{
				CommonTaskDetail: &openDpsV1alpha1Resource.CommonTaskDetail{
					Related: &openDpsV1alpha1Resource.CommonTaskDetail_Event{
						Event: moment.Name,
					},
				},
			},
		}
		task.GetCommonTaskDetail().SetEvent(moment.Name)

		upsertedTask, err := reqClient.UpsertTask(rc.ProjectName, task)
		if err != nil {
			log.Errorf("create task failed: %v", err)
		} else {
			// Update moment's task name with created task name
			moment.Task.Name = upsertedTask.GetName()
			err = rc.Save()
			if err != nil {
				log.Errorf("save record cache failed: %v", err)
			}
			if moment.Task.Name != "" && moment.Task.SyncTask {
				t, err := reqClient.SyncTask(rc.ProjectName, moment.Task.Name)
				if err == nil {
					log.Infof("sync task %s succeeded,response: %s", moment.Task.Name, t.GetName())
				}
			}
		}
	}
}

func createRelatedRecordResources(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, deviceConfig config.DeviceConfig) {
	if rc.Record["name"] == nil {
		createRecord(deviceInfo, rc, reqClient)
	}

	createRecordRelatedMoments(deviceInfo, rc, reqClient, deviceConfig)
	createRecordRelatedDiagnosisTasks(deviceInfo, rc, reqClient, deviceConfig)
	createRecordRelatedUploadTasks(deviceInfo, rc, reqClient, deviceConfig)
}

func createRecordRelatedMoments(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, deviceConfig config.DeviceConfig) {
	if len(rc.Moments) == 0 {
		return
	}

	recordName, ok := rc.Record["name"].(string)
	if !ok || recordName == "" {
		return
	}
	recordTitle, _ := rc.Record["title"].(string)
	rc, err := rc.Reload()
	if err != nil {
		log.Errorf("reload record cache failed: %v", err)
		return
	}

	for i := range rc.Moments {
		obtainEventFromMoment(deviceInfo, rc, &rc.Moments[i], recordName, recordTitle, reqClient)
		triggerDeviceEventFromMoment(deviceInfo, rc, &rc.Moments[i], recordName, reqClient, &deviceConfig)
		upsertTaskFromMoment(rc, &rc.Moments[i], recordTitle, reqClient)
	}
}

func createRecordRelatedDiagnosisTasks(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, _ config.DeviceConfig) {
	if rc.DiagnosisTask == nil {
		return
	}

	taskName, ok := rc.DiagnosisTask["name"].(string)
	if ok && taskName != "" {
		return
	}

	rc, err := rc.Reload()
	if err != nil {
		log.Errorf("reload record cache failed: %v", err)
		return
	}
	recordName, ok := rc.Record["name"].(string)
	if !ok || recordName == "" {
		return
	}
	recordTitle, _ := rc.Record["title"].(string)

	diaTask := openDpsV1alpha1Resource.Task{
		Title:       recordTitle,
		Description: getRecordDescription(recordTitle, rc),
		Category:    openDpsV1alpha1Enum.TaskCategoryEnum_DIAGNOSIS,
		State:       openDpsV1alpha1Enum.TaskStateEnum_PROCESSING,
		Tags: map[string]string{
			"recordName": recordName,
			"totalFiles": strconv.Itoa(len(rc.OriginalFiles)),
		},
	}
	diagnosisTaskDetail := openDpsV1alpha1Resource.DiagnosisTaskDetail{
		Device: deviceInfo.GetName(),
	}

	ruleName, ok := rc.DiagnosisTask["rule_name"].(string)
	if ok && ruleName != "" {
		diagnosisTaskDetail.DiagnosisRule = ruleName
	}

	ruleDisplayName, ok := rc.DiagnosisTask["rule_display_name"].(string)
	if ok && ruleDisplayName != "" {
		diagnosisTaskDetail.DisplayName = ruleDisplayName
	}

	startTime, ok := rc.DiagnosisTask["start_time"].(float64)
	if ok {
		sec, nsec := utils.NormalizeFloatTimestamp(startTime)
		diagnosisTaskDetail.StartTime = &timestamppb.Timestamp{
			Seconds: sec,
			Nanos:   nsec,
		}
	}
	endTime, ok := rc.DiagnosisTask["end_time"].(float64)
	if ok {
		sec, nsec := utils.NormalizeFloatTimestamp(endTime)
		diagnosisTaskDetail.EndTime = &timestamppb.Timestamp{
			Seconds: sec,
			Nanos:   nsec,
		}
	}
	triggerTime, ok := rc.DiagnosisTask["trigger_time"].(float64)
	if ok {
		sec, nsec := utils.NormalizeFloatTimestamp(triggerTime)
		diagnosisTaskDetail.TriggerTime = &timestamppb.Timestamp{
			Seconds: sec,
			Nanos:   nsec,
		}
	}

	diaTask.SetDiagnosisTaskDetail(&diagnosisTaskDetail)
	task, err := reqClient.CreateTask(rc.ProjectName, &diaTask)
	if err != nil {
		log.Errorf("create task failed: %v", err)
	} else {
		rc.DiagnosisTask["name"] = task.GetName()
		err = rc.Save()
		if err != nil {
			log.Errorf("save record cache failed: %v", err)
		}
	}
}

func createRecordRelatedUploadTasks(_ *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, _ config.DeviceConfig) {
	if rc.UploadTask == nil {
		return
	}

	rc, err := rc.Reload()
	if err != nil {
		log.Errorf("reload record cache failed: %v", err)
		return
	}
	taskName, ok := rc.UploadTask["name"].(string)
	if !ok || taskName == "" {
		return
	}

	recordName, ok := rc.Record["name"].(string)
	if !ok || recordName == "" {
		return
	}

	tags := map[string]string{}
	tags["recordName"] = recordName

	_, err = reqClient.AddTaskTags(taskName, tags)
	if err != nil {
		log.Errorf("add task tags failed: %v", err)
	}
}

func createRecord(deviceInfo *openDpsV1alpha1Resource.Device, recordCache *model.RecordCache, reqClient *api.RequestClient) {
	if recordCache.Record["name"] != nil {
		return
	}

	title := getRecordTitle(recordCache)
	description := getRecordDescription(title, recordCache)

	labels := make([]*openDpsV1alpha1Resource.Label, 0)
	for _, label := range recordCache.Labels {
		labels = append(labels, &openDpsV1alpha1Resource.Label{
			DisplayName: label,
		})
	}

	metadata := make(map[string]string)
	t := time.Now()
	zone, offset := t.Zone()
	metadata["deviceTimezoneOffset"] = strconv.Itoa(offset)
	metadata["deviceTimezone"] = zone

	record := &openDpsV1alpha1Resource.Record{
		Title:       title,
		Description: description,
		Labels:      labels,
		Device:      deviceInfo,
		Metadata:    metadata,
	}
	ruleName, ok := recordCache.DiagnosisTask["rule_name"].(string)
	if ok {
		record.Rules = []*openDpsV1alpha1Resource.DiagnosisRule{
			{
				Name: ruleName,
			},
		}
	}

	record, err := reqClient.CreateRecord(recordCache.ProjectName, record)
	if err != nil {
		log.Errorf("create record failed: %v", err)
	}
	recordCache.Record = map[string]interface{}{
		"name":        record.GetName(),
		"title":       record.GetTitle(),
		"description": record.GetDescription(),
	}

	err = recordCache.Save()
	if err != nil {
		log.Errorf("save record cache failed: %v", err)
	}
}

func getRecordTitle(rc *model.RecordCache) string {
	title, ok := rc.Record["title"].(string)
	if ok && title != "" {
		return title
	}

	taskTitle, ok := rc.UploadTask["title"].(string)
	if ok && taskTitle != "" {
		return taskTitle
	}

	triggerTime := time.Unix(rc.Timestamp/1000, 0).Format(time.RFC3339)
	return "Record at " + triggerTime
}

func getRecordDescription(title string, rc *model.RecordCache) string {
	desc, ok := rc.Record["description"].(string)
	if ok && desc != "" {
		return desc
	}

	return title
}

func checkRecordCacheExpired(expiredHours int, timestamp int64, rcPath string) bool {
	if expiredHours < 0 {
		return false
	}

	fileTime := time.UnixMilli(timestamp)
	if time.Since(fileTime).Hours() > float64(expiredHours) {
		parentFolder := utils.GetParentFolder(utils.GetParentFolder(rcPath))
		return utils.DeleteDir(parentFolder)
	}
	return false
}

func getDeviceExtraInfos(extraFiles []string) *structpb.Value {
	extraInfoMap := make(map[string]interface{})

	for _, file := range extraFiles {
		if !utils.CheckReadPath(file) {
			log.Warnf("extra file %s not exist or has no permission", file)
			continue
		}

		if !strings.HasSuffix(file, ".yaml") && !strings.HasSuffix(file, ".yml") {
			continue
		}

		data, err := os.ReadFile(file)
		if err != nil {
			log.Errorf("read extra file %s failed: %v", file, err)
			continue
		}

		var yamlData map[string]interface{}
		if err := yaml.Unmarshal(data, &yamlData); err != nil {
			log.Errorf("unmarshal yaml file %s failed: %v", file, err)
			continue
		}

		for k, v := range yamlData {
			extraInfoMap[k] = v
		}
	}

	value, err := structpb.NewStruct(extraInfoMap)
	if err != nil {
		log.Errorf("convert extra info to structpb failed: %v", err)
		return nil
	}

	return structpb.NewStructValue(value)
}
