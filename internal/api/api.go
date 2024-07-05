package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	openDpsV1alpha1Connect "buf.build/gen/go/coscene-io/coscene-openapi/connectrpc/go/coscene/openapi/dataplatform/v1alpha1/services/servicesconnect"
	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	openDpsV1alpha1Service "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/services"
	"connectrpc.com/connect"
	"github.com/coscene-io/cos-agent/internal/config"
	"github.com/coscene-io/cos-agent/internal/storage"
	"github.com/coscene-io/cos-agent/pkg/constant"
	log "github.com/sirupsen/logrus"
)

type RequestClient struct {
	storage   storage.Storage
	deviceCli openDpsV1alpha1Connect.DeviceServiceClient
}

func NewRequestClient(apiConfig config.ApiConfig, storage storage.Storage) *RequestClient {
	httpClient := http.DefaultClient
	deviceClient := openDpsV1alpha1Connect.NewDeviceServiceClient(httpClient, apiConfig.ServerURL)

	return &RequestClient{
		deviceCli: deviceClient,
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

func (r *RequestClient) getAuthToken() string {
	bytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey))
	if err != nil {
		log.Errorf("unable to get auth token: %v", err)
		return ""
	}
	return string(bytes)
}
