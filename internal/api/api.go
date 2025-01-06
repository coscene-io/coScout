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

package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	openAnaV1alpha1Connect "buf.build/gen/go/coscene-io/coscene-openapi/connectrpc/go/coscene/openapi/analysis/v1alpha1/services/servicesconnect"
	openDpsV1alpha1Connect "buf.build/gen/go/coscene-io/coscene-openapi/connectrpc/go/coscene/openapi/dataplatform/v1alpha1/services/servicesconnect"
	openStorV1alpha1Connect "buf.build/gen/go/coscene-io/coscene-openapi/connectrpc/go/coscene/openapi/datastorage/v1alpha1/services/servicesconnect"
	openAnaV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/resources"
	openAnaV1alpha1Service "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/analysis/v1alpha1/services"
	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	openDpsV1alpha1Service "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/services"
	openStorV1alpha1Service "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/datastorage/v1alpha1/services"
	"connectrpc.com/connect"
	"github.com/coscene-io/coscout/internal/api/interceptor"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type RequestClient struct {
	networkChan      chan *model.NetworkUsage
	storage          storage.Storage
	deviceCli        openDpsV1alpha1Connect.DeviceServiceClient
	configCli        openDpsV1alpha1Connect.ConfigMapServiceClient
	taskCli          openDpsV1alpha1Connect.TaskServiceClient
	rcdCli           openDpsV1alpha1Connect.RecordServiceClient
	eventCli         openDpsV1alpha1Connect.EventServiceClient
	deviceEventCli   openAnaV1alpha1Connect.DeviceEventServiceClient
	securityTokenCli openStorV1alpha1Connect.SecurityTokenServiceClient
	fileCli          openDpsV1alpha1Connect.FileServiceClient
	labelCli         openDpsV1alpha1Connect.LabelServiceClient
}

func NewRequestClient(apiConfig config.ApiConfig, storage storage.Storage, networkChan chan *model.NetworkUsage) *RequestClient {
	httpClient := http.DefaultClient
	interceptors := connect.WithInterceptors(interceptor.NetworkUsageInterceptor(networkChan))

	deviceClient := openDpsV1alpha1Connect.NewDeviceServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	configClient := openDpsV1alpha1Connect.NewConfigMapServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	taskClient := openDpsV1alpha1Connect.NewTaskServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	recordClient := openDpsV1alpha1Connect.NewRecordServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	eventClient := openDpsV1alpha1Connect.NewEventServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	deviceEventClient := openAnaV1alpha1Connect.NewDeviceEventServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	securityTokenClient := openStorV1alpha1Connect.NewSecurityTokenServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	fileClient := openDpsV1alpha1Connect.NewFileServiceClient(httpClient, apiConfig.ServerURL, interceptors)
	labelClient := openDpsV1alpha1Connect.NewLabelServiceClient(httpClient, apiConfig.ServerURL, interceptors)

	return &RequestClient{
		deviceCli:        deviceClient,
		configCli:        configClient,
		taskCli:          taskClient,
		rcdCli:           recordClient,
		eventCli:         eventClient,
		deviceEventCli:   deviceEventClient,
		securityTokenCli: securityTokenClient,
		fileCli:          fileClient,
		labelCli:         labelClient,
		storage:          storage,
		networkChan:      networkChan,
	}
}

func (r *RequestClient) GetNetworkChan() chan *model.NetworkUsage {
	return r.networkChan
}

func (r *RequestClient) RegisterDevice(device *openDpsV1alpha1Resource.Device, orgSlug, projectSlug string) (*openDpsV1alpha1Resource.Device, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.RegisterDeviceRequest{
		Device: device,
	}
	if orgSlug != "" {
		req.Project = &openDpsV1alpha1Service.RegisterDeviceRequest_OrganizationSlug{
			OrganizationSlug: orgSlug,
		}
	}
	if projectSlug != "" {
		req.Project = &openDpsV1alpha1Service.RegisterDeviceRequest_ProjectSlug{
			ProjectSlug: projectSlug,
		}
	}

	apiReq := connect.NewRequest(&req)
	apiRes, err := r.deviceCli.RegisterDevice(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to register device: %v", err)
		return nil, "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to register device"))
	}

	return apiRes.Msg.GetDevice(), apiRes.Msg.GetExchangeCode(), nil
}

func (r *RequestClient) CheckDeviceStatus(device string, exchangeCode string) (exist bool, state openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_DeviceAuthorizeState, e error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.CheckDeviceStatusRequest{
		Device:       device,
		ExchangeCode: exchangeCode,
	}
	apiReq := connect.NewRequest(&req)

	apiRes, err := r.deviceCli.CheckDeviceStatus(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to check device status: %v", err)
		return false, openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_DEVICE_AUTHORIZE_STATE_UNSPECIFIED,
			connect.NewError(connect.CodeInternal, errors.New("unable to update device"))
	}

	return apiRes.Msg.GetExist(), apiRes.Msg.GetAuthorizeState(), nil
}

func (r *RequestClient) ExchangeDeviceAuthToken(device string, exchangeCode string) (string, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.ExchangeDeviceAuthTokenRequest{
		Device:       device,
		ExchangeCode: exchangeCode,
	}
	apiReq := connect.NewRequest(&req)

	apiRes, err := r.deviceCli.ExchangeDeviceAuthToken(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to exchange device auth token: %v", err)
		return "", 0, connect.NewError(connect.CodeInternal, errors.New("unable to exchange device auth token"))
	}

	return apiRes.Msg.GetDeviceAuthToken(), apiRes.Msg.GetExpiresTime().GetSeconds(), nil
}

func (r *RequestClient) GetDevice(name string) (*openDpsV1alpha1Resource.Device, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.GetDeviceRequest{
		Name: name,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.deviceCli.GetDevice(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to get device: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to get device"))
	}

	return apiRes.Msg, nil
}

func (r *RequestClient) SendHeartbeat(deviceName string, cosVersion string, networks *openDpsV1alpha1Service.NetworkUsage, extraInfo map[string]string) (*emptypb.Empty, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.HeartbeatDeviceRequest{
		Name:         deviceName,
		NetworkUsage: networks,
		ExtraInfo:    extraInfo,
	}
	if cosVersion != "" {
		req.CosVersion = cosVersion
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.deviceCli.HeartbeatDevice(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to send device heartbeat: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to send device heartbeat"))
	}

	return apiRes.Msg, nil
}

func (r *RequestClient) GetConfigMapWithCache(name string) (*openDpsV1alpha1Resource.ConfigMap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	configMap := openDpsV1alpha1Resource.ConfigMap{}
	configStr, err := r.storage.Get([]byte(constant.DeviceRemoteCacheBucket), []byte(name))
	if err == nil && len(configStr) > 0 {
		err = protojson.Unmarshal(configStr, &configMap)
		if err != nil {
			log.Errorf("unable to unmarshal config map from cache: %v", err)
		}
	}

	metadataRequest := openDpsV1alpha1Service.GetConfigMapMetadataRequest{
		Name: name,
	}
	metadataApiReq := connect.NewRequest(&metadataRequest)
	metadataApiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())
	metadata, err := r.configCli.GetConfigMapMetadata(ctx, metadataApiReq)
	if err != nil {
		log.Errorf("unable to get config map metadata: %v", err)
		return &configMap, nil
	}

	if configMap.GetMetadata() != nil && (configMap.GetMetadata().GetCurrentVersion() == metadata.Msg.GetCurrentVersion()) {
		return &configMap, nil
	}

	req := openDpsV1alpha1Service.GetConfigMapRequest{
		Name: name,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.configCli.GetConfigMap(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to get config map: %v", err)
		return &configMap, nil
	}
	bytes, err := protojson.Marshal(apiRes.Msg)
	if err != nil {
		log.Errorf("unable to marshal config map: %v", err)
		return apiRes.Msg, nil
	}
	err = r.storage.Put([]byte(constant.DeviceRemoteCacheBucket), []byte(name), bytes)
	if err != nil {
		log.Errorf("unable to put config map to cache: %v", err)
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) ListDeviceTasks(deviceName string, state *openDpsV1alpha1Enum.TaskStateEnum_TaskState) ([]*openDpsV1alpha1Resource.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.ListDeviceTasksRequest{
		Parent:   deviceName,
		PageSize: 10,
		Filter:   fmt.Sprintf("state=%s AND category=UPLOAD", state.String()),
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.taskCli.ListDeviceTasks(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to list device tasks: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to list device tasks"))
	}

	return apiRes.Msg.GetDeviceTasks(), nil
}

func (r *RequestClient) UpdateTaskState(name string, state *openDpsV1alpha1Enum.TaskStateEnum_TaskState) (*openDpsV1alpha1Resource.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.UpdateTaskRequest{
		Task: &openDpsV1alpha1Resource.Task{
			Name:  name,
			State: *state,
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"state"},
		},
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.taskCli.UpdateTask(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to update task state: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to update task state"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) AddTaskTags(task string, tags map[string]string) (*emptypb.Empty, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.AddTaskTagsRequest{
		Task: task,
		Tags: tags,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.taskCli.AddTaskTags(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to add task tags: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to add task tags"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) CreateTask(projectName string, task *openDpsV1alpha1Resource.Task) (*openDpsV1alpha1Resource.Task, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.CreateTaskRequest{
		Parent: projectName,
		Task:   task,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.taskCli.CreateTask(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to create task: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("unable to create task"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) CreateRecord(parent string, rc *openDpsV1alpha1Resource.Record) (*openDpsV1alpha1Resource.Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.CreateRecordRequest{
		Parent: parent,
		Record: rc,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.rcdCli.CreateRecord(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to save record cache: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("unable to save record cache"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) TriggerDeviceEvent(projectName string, deviceEvent *openAnaV1alpha1Resource.DeviceEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openAnaV1alpha1Service.TriggerDeviceEventsRequest{
		Parent:       projectName,
		DeviceEvents: []*openAnaV1alpha1Resource.DeviceEvent{deviceEvent},
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	_, err := r.deviceEventCli.TriggerDeviceEvents(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to trigger device events: %v", err)
		return connect.NewError(connect.CodeInternal, errors.New("unable to trigger device events"))
	}
	return nil
}

func (r *RequestClient) ObtainEvent(projectName string, event *openDpsV1alpha1Resource.Event) (*openDpsV1alpha1Resource.Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.ObtainEventRequest{
		Parent: projectName,
		Event:  event,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.eventCli.ObtainEvent(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to obtain event: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to obtain event"))
	}

	return apiRes.Msg.GetEvent(), nil
}

func (r *RequestClient) GenerateSecurityToken(project string) (*openStorV1alpha1Service.GenerateSecurityTokenResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openStorV1alpha1Service.GenerateSecurityTokenRequest{
		Project: project,
		ExpireDuration: &durationpb.Duration{
			Seconds: 24 * 60 * 60,
		},
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.securityTokenCli.GenerateSecurityToken(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to generate security token: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("unable to generate security token"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) CheckCloneFile(recordName, fileName, sha256 string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.CloneFileRequest{
		Parent: recordName,
		File:   fileName,
		Sha256: sha256,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	_, err := r.fileCli.CloneFile(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to check clone file: %v", err)
		return false
	}
	return true
}

func (r *RequestClient) UpdateRecordLabels(projectName, recordName string, labels []string) (*openDpsV1alpha1Resource.Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	createdLabels := make([]*openDpsV1alpha1Resource.Label, 0)
	for _, label := range labels {
		l, err := r.ensureLabel(projectName, label)
		if err != nil {
			return nil, err
		}
		createdLabels = append(createdLabels, l)
	}

	req := openDpsV1alpha1Service.UpdateRecordRequest{
		Record: &openDpsV1alpha1Resource.Record{
			Name:   recordName,
			Labels: createdLabels,
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"labels"},
		},
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.rcdCli.UpdateRecord(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to update record: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to update record"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) ensureLabel(projectName string, displayName string) (*openDpsV1alpha1Resource.Label, error) {
	label, err := r.getLabel(projectName, displayName)
	if err != nil {
		return nil, err
	}
	if label != nil && label.GetName() != "" {
		return label, nil
	}
	label, err = r.addLabel(projectName, displayName)
	return label, err
}

func (r *RequestClient) getLabel(projectName string, displayName string) (*openDpsV1alpha1Resource.Label, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.ListLabelsRequest{
		Parent:   projectName,
		Filter:   "displayName=" + displayName,
		PageSize: 10,
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.labelCli.ListLabels(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to list labels: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("unable to list labels"))
	}
	for _, label := range apiRes.Msg.GetLabels() {
		if label.GetDisplayName() == displayName {
			return label, nil
		}
	}
	return &openDpsV1alpha1Resource.Label{}, nil
}

func (r *RequestClient) addLabel(projectName string, displayName string) (*openDpsV1alpha1Resource.Label, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openDpsV1alpha1Service.CreateLabelRequest{
		Parent: projectName,
		Label: &openDpsV1alpha1Resource.Label{
			DisplayName: displayName,
		},
	}
	apiReq := connect.NewRequest(&req)
	apiReq.Header().Set(constant.AuthHeaderKey, r.getAuthToken())

	apiRes, err := r.labelCli.CreateLabel(ctx, apiReq)
	if err != nil {
		log.Errorf("unable to create label: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "unable to create label"))
	}
	return apiRes.Msg, nil
}

func (r *RequestClient) getAuthToken() string {
	bytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey))
	if err != nil {
		log.Errorf("unable to get auth token: %v", err)
		return ""
	}
	return constant.BasicAuthPrefix + " " + base64.StdEncoding.EncodeToString([]byte(constant.BasicAuthUsername+":"+string(bytes)))
}
