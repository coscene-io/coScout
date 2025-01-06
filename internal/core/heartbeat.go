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

package core

import (
	"context"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/services"
	"github.com/coscene-io/coscout"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
)

const heartbeatInterval = 60 * time.Second

type ErrorInfo struct {
	Err       error
	Timestamp int64
}

func SendHeartbeat(ctx context.Context, reqClient *api.RequestClient, storage *storage.Storage, errChan chan error) {
	deviceInfo := GetDeviceInfo(storage)
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		return
	}

	lastError := ErrorInfo{}
	go func(c chan error) {
		for err := range c {
			lastError.Err = err
			lastError.Timestamp = time.Now().Unix()
		}
	}(errChan)

	networkUsage := model.NetworkUsage{}
	go func(networkChan chan *model.NetworkUsage) {
		for {
			select {
			case nc := <-networkChan:
				networkUsage.AddSent(nc.GetTotalSent())
				networkUsage.AddReceived(nc.GetTotalReceived())
			case <-ctx.Done():
				return
			}
		}
	}(reqClient.GetNetworkChan())

	for {
		select {
		case <-ctx.Done():
			return
		default:
			extraInfo := map[string]string{}
			if lastError.Err != nil && lastError.Timestamp > time.Now().Add(-5*heartbeatInterval).Unix() {
				extraInfo["error_msg"] = lastError.Err.Error()
				extraInfo["code"] = "ERROR"
			} else {
				extraInfo["code"] = "OK"
			}

			sentBytes := networkUsage.GetTotalSent()
			receivedBytes := networkUsage.GetTotalReceived()
			nc := services.NetworkUsage{
				UploadBytes:   sentBytes,
				DownloadBytes: receivedBytes,
			}

			cosVersion := coscout.GetVersion()
			_, err := reqClient.SendHeartbeat(deviceInfo.GetName(), cosVersion, &nc, extraInfo)
			if err != nil {
				log.Errorf("failed to send heartbeat: %v", err)
			} else {
				log.Infof("device %s heartbeat sent", deviceInfo.GetDisplayName())

				networkUsage.ReduceSent(sentBytes)
				networkUsage.ReduceReceived(receivedBytes)
			}

			time.Sleep(heartbeatInterval)
		}
	}
}
