package collector

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/pkg/constant"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"

	openAnaV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/enums"
	openAnaV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/resources"
	openDpsV1alpha1Common "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/commons"
	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const checkInterval = 60 * time.Second

func SaveRecordCache(rc *model.RecordCache) error {
	baseFolder := config.GetRecordCacheFolder()

	seconds := rc.Timestamp / 1000
	milliseconds := rc.Timestamp % 1000
	dirName := time.Unix(seconds, 0).UTC().Format("2006-01-02-15-04-05") + "_" + strconv.Itoa(int(milliseconds))

	dirPath := filepath.Join(baseFolder, dirName, ".cos")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			log.Errorf("create cache directory failed: %v", err)
			return errors.New("create cache directory failed")
		}
	}

	file := filepath.Join(dirPath, "state.json")
	data, err := json.Marshal(rc)
	if err != nil {
		log.Errorf("marshal record cache failed: %v", err)
		return errors.New("marshal record cache failed")
	}
	err = os.WriteFile(file, data, 0644)
	if err != nil {
		log.Errorf("write record cache failed: %v", err)
		return errors.New("write record cache failed")
	}
	return nil
}

func FindAllRecordCaches() []string {
	baseFolder := config.GetRecordCacheFolder()
	var records []string

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

func Collect(ctx context.Context, reqClient *api.RequestClient, confManager *config.ConfManager, errorChan chan error) error {
	uploadChan := make(chan *model.RecordCache, 10)

	go Upload(ctx, reqClient, confManager, uploadChan, errorChan)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				appConfig := confManager.LoadWithRemote()
				getStorage := confManager.GetStorage()

				err := handleRecordCaches(uploadChan, reqClient, appConfig, getStorage)
				if err != nil {
					errorChan <- err
				}
			case <-ctx.Done():
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

	<-ctx.Done()
	return nil
}

func handleRecordCaches(uploadChan chan *model.RecordCache, reqClient *api.RequestClient, config *config.AppConfig, storage *storage.Storage) error {
	deviceInfo := getDeviceInfo(*storage)
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		log.Warn("device info not found, skip collecting")
		return nil
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

		uploadChan <- &rc
	}
	return nil
}

func createRelatedRecordResources(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, deviceConfig config.DeviceConfig) {
	if rc.Record["name"] == nil {
		createRecord(deviceInfo, rc, reqClient)
	}

	createRecordRelatedResources(deviceInfo, rc, reqClient, deviceConfig)
}

func createRecordRelatedResources(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient, deviceConfig config.DeviceConfig) {
	recordName, ok := rc.Record["name"].(string)
	if !ok {
		return
	}
	recordTitle, _ := rc.Record["title"].(string)

	for i := range rc.Moments {
		moment := rc.Moments[i]
		if moment.Name == "" {
			displayName := recordTitle
			description := recordTitle
			if moment.Title != "" {
				displayName = moment.Title
			}
			if moment.Description != "" {
				description = moment.Description
			}

			if moment.Timestamp > 1_000_000_000_000 {
				moment.Timestamp /= 1000
			}

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
			if moment.RuleId != "" {
				event.Rule = &openDpsV1alpha1Common.RuleSpec{
					Id: moment.RuleId,
				}
			}
			obtainEvent, err := reqClient.ObtainEvent(rc.ProjectName, &event)
			if err != nil {
				log.Errorf("obtain event failed: %v", err)
			} else {
				rc.Moments[i].Name = obtainEvent.GetName()
				rc.Moments[i].IsNew = true
			}

			err = SaveRecordCache(rc)
			if err != nil {
				log.Errorf("save record cache failed: %v", err)
			}
		}

		if rc.Moments[i].Name != "" && rc.Moments[i].Event == nil {
			m := rc.Moments[i]

			deviceEvent := openAnaV1alpha1Resource.DeviceEvent{
				Code:       m.Code,
				Parameters: m.Metadata,
				TriggerTime: &timestamppb.Timestamp{
					Seconds: int64(m.Timestamp),
					Nanos:   int32((m.Timestamp - float64(int64(m.Timestamp))) * 1e9),
				},
				Duration: &durationpb.Duration{
					Seconds: int64(m.Duration),
					Nanos:   int32((m.Duration - float64(int64(m.Duration))) * 1e9),
				},
				EventSource:     openAnaV1alpha1Enum.EventSourceEnum_DEVICE,
				Moment:          m.Name,
				Device:          deviceInfo.GetName(),
				Record:          recordName,
				DiagnosisRuleId: m.RuleId,
				DeviceContext:   getDeviceExtraInfos(deviceConfig.ExtraFiles),
			}

			err := reqClient.TriggerDeviceEvent(rc.ProjectName, &deviceEvent)
			if err != nil {
				log.Errorf("trigger device event failed: %v", err)
			}
		}

	}

	if rc.DiagnosisTask != nil {
		taskName, ok := rc.DiagnosisTask["name"].(string)
		if ok && taskName != "" {
			return
		}

		diaTask := openDpsV1alpha1Resource.Task{
			Title:       recordTitle,
			Description: getRecordDescription(recordTitle, rc),
			Category:    openDpsV1alpha1Enum.TaskCategoryEnum_DIAGNOSIS,
			State:       openDpsV1alpha1Enum.TaskStateEnum_PROCESSING,
			Tags: map[string]string{
				"recordName": recordName,
			},
		}
		diaTask.SetDiagnosisTaskDetail(&openDpsV1alpha1Resource.DiagnosisTaskDetail{
			Device: deviceInfo.GetName(),
			StartTime: &timestamppb.Timestamp{
				Seconds: rc.DiagnosisTask["start_time"].(int64),
			},
			EndTime: &timestamppb.Timestamp{
				Seconds: rc.DiagnosisTask["end_time"].(int64),
			},
			RuleSpecId:  rc.DiagnosisTask["rule_id"].(string),
			DisplayName: recordTitle,
			TriggerTime: &timestamppb.Timestamp{
				Seconds: rc.DiagnosisTask["trigger_time"].(int64),
			},
		})
		task, err := reqClient.CreateTask(rc.ProjectName, &diaTask)
		if err != nil {
			log.Errorf("create task failed: %v", err)
		} else {
			rc.DiagnosisTask["name"] = task.GetName()
			err = SaveRecordCache(rc)
			if err != nil {
				log.Errorf("save record cache failed: %v", err)
			}
		}
	}
}

func createRecord(deviceInfo *openDpsV1alpha1Resource.Device, rc *model.RecordCache, reqClient *api.RequestClient) {
	if rc.Record["name"] != nil {
		return
	}

	title := getRecordTitle(rc)
	description := getRecordDescription(title, rc)

	labels := make([]*openDpsV1alpha1Resource.Label, 0)
	for _, label := range rc.Labels {
		labels = append(labels, &openDpsV1alpha1Resource.Label{
			DisplayName: label,
		})
	}

	record := &openDpsV1alpha1Resource.Record{
		Title:       title,
		Description: description,
		Labels:      labels,
		Device:      deviceInfo,
	}
	ruleId, ok := rc.DiagnosisTask["rule_id"].(string)
	if ok {
		ruleSpec := &openDpsV1alpha1Common.RuleSpec{
			Id: ruleId,
		}
		record.Rules = []*openDpsV1alpha1Common.RuleSpec{ruleSpec}
	}

	record, err := reqClient.CreateRecord(rc.ProjectName, record)
	if err != nil {
		log.Errorf("create record failed: %v", err)
	}
	rc.Record = map[string]interface{}{
		"name":        record.GetName(),
		"title":       record.GetTitle(),
		"description": record.GetDescription(),
	}

	err = SaveRecordCache(rc)
	if err != nil {
		log.Errorf("save record cache failed: %v", err)
	}
}

func getRecordTitle(rc *model.RecordCache) string {
	if rc.Record["title"] != nil {
		return rc.Record["title"].(string)
	}

	if rc.UploadTask["title"] != nil {
		return rc.UploadTask["title"].(string)
	}

	triggerTime := time.Unix(rc.Timestamp/1000, 0).Format(time.RFC3339)
	return "Record at " + triggerTime
}

func getRecordDescription(title string, rc *model.RecordCache) string {
	if rc.Record["description"] != nil {
		return rc.Record["description"].(string)
	}

	return title
}

func checkRecordCacheExpired(expiredHours int, timestamp int64, rcPath string) bool {
	fileTime := time.UnixMilli(timestamp)
	if time.Since(fileTime).Hours() > float64(expiredHours) {
		parentFolder := utils.GetParentFolder(utils.GetParentFolder(rcPath))
		return utils.DeleteDir(parentFolder)
	}
	return false
}

func getDeviceInfo(storage storage.Storage) *openDpsV1alpha1Resource.Device {
	bytes, err := (storage).Get([]byte(constant.DeviceMetadataBucket), []byte(constant.DeviceInfoKey))
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

func getDeviceExtraInfos(extraFiles []string) *structpb.Value {
	extraInfo := structpb.Value{}

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

		err = protojson.Unmarshal(data, &extraInfo)
		if err != nil {
			log.Errorf("unmarshal extra file %s failed: %v", file, err)
			continue
		}
	}
	return &extraInfo
}
