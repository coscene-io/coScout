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

package register

import (
	"strconv"
	"time"

	openDpsV1alpha1Enum "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	deviceAuthCheckInterval = 60 * time.Second
)

type ModRegister interface {
	// GetDevice returns the device information for different providers
	GetDevice() *openDpsV1alpha1Resource.Device
}

func NewModRegister(conf config.RegisterConfig) (ModRegister, error) {
	switch conf.Provider {
	case constant.RegisterProviderFile:
		return NewFileModRegister(conf.Conf), nil
	default:
		log.Errorf("Invalid register provider: %s", conf.Provider)
	}
	return nil, errors.New("invalid register provider")
}

type DeviceStatusResponse struct {
	Authorized bool `json:"authorized"`
	Exist      bool `json:"exist"`
}

type Register struct {
	reqClient api.RequestClient
	config    config.AppConfig
	storage   storage.Storage
}

func NewRegister(reqClient api.RequestClient, config config.AppConfig, storage storage.Storage) *Register {
	return &Register{
		reqClient: reqClient,
		config:    config,
		storage:   storage,
	}
}

func (r *Register) CheckOrRegisterDevice(channel chan<- DeviceStatusResponse) {
	for {
		device := core.GetDeviceInfo(&r.storage)
		// If device is not registered, register it
		if device == nil || device.GetName() == "" {
			modRegister, err := NewModRegister(r.config.Register)
			if err != nil {
				channel <- DeviceStatusResponse{
					Authorized: false,
					Exist:      false,
				}

				time.Sleep(deviceAuthCheckInterval)
				continue
			}

			isSucceed, localDevice := r.registerDevice(modRegister.GetDevice())
			if !isSucceed {
				channel <- DeviceStatusResponse{
					Authorized: false,
					Exist:      false,
				}

				time.Sleep(deviceAuthCheckInterval)
				continue
			}
			device = localDevice
		}

		log.Infof("Current device serial number: %s", device.GetSerialNumber())

		// check device status
		exist, state, err := r.getRemoteDeviceStatus(device.GetName())
		if err != nil {
			channel <- DeviceStatusResponse{
				Authorized: false,
				Exist:      false,
			}

			time.Sleep(deviceAuthCheckInterval)
			continue
		}

		if !exist {
			channel <- DeviceStatusResponse{
				Authorized: false,
				Exist:      false,
			}

			log.Infof("Remote device %s already deleted.", device.GetSerialNumber())
			time.Sleep(deviceAuthCheckInterval * 10)
			continue
		}

		if state == openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_REJECTED {
			channel <- DeviceStatusResponse{
				Authorized: false,
				Exist:      true,
			}

			log.Infof("Remote device %s already be rejected", device.GetSerialNumber())
			err := r.storage.Delete([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExpireKey))
			if err != nil {
				log.Warnf("unable to delete device auth token expireTime: %v", err)
			}

			err = r.storage.Delete([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey))
			if err != nil {
				log.Warnf("unable to delete device auth token: %v", err)
			}
			time.Sleep(deviceAuthCheckInterval)
			continue
		}

		if state == openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_PENDING {
			channel <- DeviceStatusResponse{
				Authorized: false,
				Exist:      true,
			}

			log.Infof("Remote device %s is pending", device.GetSerialNumber())
			time.Sleep(deviceAuthCheckInterval)
			continue
		}

		// If device is approved, exchange auth token
		if state == openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_APPROVED {
			// Check device auth token
			valid := r.checkAuthToken()
			if valid {
				channel <- DeviceStatusResponse{
					Authorized: true,
					Exist:      true,
				}

				time.Sleep(deviceAuthCheckInterval)
				continue
			}

			isSucceed := r.exchangeAuthToken(device.GetName())
			if !isSucceed {
				channel <- DeviceStatusResponse{
					Authorized: false,
					Exist:      true,
				}
			} else {
				channel <- DeviceStatusResponse{
					Authorized: true,
					Exist:      true,
				}
			}
		}

		time.Sleep(deviceAuthCheckInterval)
		continue
	}
}

func (r *Register) registerDevice(device *openDpsV1alpha1Resource.Device) (isSucceed bool, d *openDpsV1alpha1Resource.Device) {
	if device == nil {
		return false, &openDpsV1alpha1Resource.Device{}
	}

	remoteDevice, exchangeCode, err := r.reqClient.RegisterDevice(device, r.config.Api.OrgSlug, r.config.Api.ProjectSlug)
	if err != nil {
		log.Warnf("unable to register device: %v", err)
		return false, &openDpsV1alpha1Resource.Device{}
	}

	err = r.setDeviceInfo(remoteDevice, exchangeCode)
	if err != nil {
		log.Warnf("unable to set device info: %v", err)
		return false, &openDpsV1alpha1Resource.Device{}
	}
	return true, remoteDevice
}

func (r *Register) checkAuthToken() (valid bool) {
	bytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExpireKey))
	if err != nil {
		log.Warnf("unable to get device auth token expireTime: %v", err)
		return false
	}
	if len(bytes) == 0 {
		return false
	}

	expireTime, err := strconv.ParseInt(string(bytes), 10, 64)
	if err != nil {
		log.Warnf("unable to parse device auth token expireTime: %v", err)
		return false
	}

	// If the token is expired, return false
	if time.Unix(expireTime, 0).Before(time.Now().Add(-6 * time.Hour)) {
		return false
	}
	return true
}

func (r *Register) setAuthToken(token string, expireTime int64) error {
	err := r.storage.Put([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthKey), []byte(token))
	if err != nil {
		return err
	}

	return r.storage.Put([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExpireKey), []byte(strconv.FormatInt(expireTime, 10)))
}

func (r *Register) getRemoteDeviceStatus(device string) (exist bool, state openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_DeviceAuthorizeState, err error) {
	bytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExchangeCodeKey))
	if err != nil {
		log.Errorf("unable to get auth token: %v", err)
		return false, openDpsV1alpha1Enum.DeviceAuthorizeStateEnum_DEVICE_AUTHORIZE_STATE_UNSPECIFIED, err
	}

	exchangeCode := string(bytes)
	return r.reqClient.CheckDeviceStatus(device, exchangeCode)
}

func (r *Register) exchangeAuthToken(device string) (isSucceed bool) {
	exchangeCodeBytes, err := r.storage.Get([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExchangeCodeKey))
	if err != nil {
		log.Warnf("unable to get device auth token: %v", err)
		return false
	}

	token, expireTime, err := r.reqClient.ExchangeDeviceAuthToken(device, string(exchangeCodeBytes))
	if err != nil {
		log.Warnf("unable to exchange device auth token: %v", err)
		return false
	}

	err = r.setAuthToken(token, expireTime)
	if err != nil {
		log.Warnf("unable to set device auth token: %v", err)
		return false
	}

	return true
}

func (r *Register) setDeviceInfo(device *openDpsV1alpha1Resource.Device, exchangeCode string) error {
	bytes, err := protojson.Marshal(device)
	if err != nil {
		return err
	}

	err = r.storage.Put([]byte(constant.DeviceMetadataBucket), []byte(constant.DeviceInfoKey), bytes)
	if err != nil {
		return err
	}

	return r.storage.Put([]byte(constant.DeviceAuthBucket), []byte(constant.DeviceAuthExchangeCodeKey), []byte(exchangeCode))
}
