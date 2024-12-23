package api

import (
	"context"
	"encoding/base64"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"net/http"
	"time"

	openDpsV1alpha1Connect "buf.build/gen/go/coscene-io/coscene-openapi/connectrpc/go/coscene/openapi/dataplatform/v1alpha1/services/servicesconnect"
	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	openDpsV1alpha1Service "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/services"
	"connectrpc.com/connect"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
)

type RequestClient struct {
	storage   storage.Storage
	deviceCli openDpsV1alpha1Connect.DeviceServiceClient
	configCli openDpsV1alpha1Connect.ConfigMapServiceClient
}

func NewRequestClient(apiConfig config.ApiConfig, storage storage.Storage) *RequestClient {
	httpClient := http.DefaultClient
	deviceClient := openDpsV1alpha1Connect.NewDeviceServiceClient(httpClient, apiConfig.ServerURL)
	configClient := openDpsV1alpha1Connect.NewConfigMapServiceClient(httpClient, apiConfig.ServerURL)

	return &RequestClient{
		deviceCli: deviceClient,
		configCli: configClient,
		storage:   storage,
	}
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
		return nil, "", connect.NewError(connect.CodeInternal, errors.New("unable to register device"))
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
		return nil, connect.NewError(connect.CodeInternal, errors.New("unable to get device"))
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

	if configMap.GetMetadata() != nil && (configMap.GetMetadata().CurrentVersion == metadata.Msg.GetCurrentVersion()) {
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

func (r *RequestClient) getAuthToken() string {
	bytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey))
	if err != nil {
		log.Errorf("unable to get auth token: %v", err)
		return ""
	}
	return constant.BasicAuthPrefix + " " + base64.StdEncoding.EncodeToString([]byte(constant.BasicAuthUsername+":"+string(bytes)))
}
