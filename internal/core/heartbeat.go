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
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
)

type ErrorInfo struct {
	Err       error
	Timestamp int64
}

func SendHeartbeat(ctx context.Context, reqClient *api.RequestClient, storage *storage.Storage, errChan chan error) {
	lastError := ErrorInfo{}
	go func(c chan error) {
		for {
			select {
			case err := <-c:
				log.Errorf("runtime error: %v", err)

				lastError.Err = err
				lastError.Timestamp = time.Now().Unix()
			case <-ctx.Done():
				return
			}
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
			deviceInfo := GetDeviceInfo(storage)
			if deviceInfo == nil || deviceInfo.GetName() == "" {
				log.Warn("device info is not available, skipping heartbeat")

				time.Sleep(config.HeartbeatInterval)
				continue
			}

			extraInfo := map[string]string{}
			if lastError.Err != nil && lastError.Timestamp > time.Now().Add(-5*config.HeartbeatInterval).Unix() {
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
			//nolint: contextcheck //context is checked in the parent goroutine
			_, err := reqClient.SendHeartbeat(deviceInfo.GetName(), cosVersion, &nc, extraInfo)
			if err != nil {
				log.Errorf("failed to send heartbeat: %v", err)
			} else {
				log.Infof("device %s heartbeat sent", deviceInfo.GetDisplayName())

				networkUsage.ReduceSent(sentBytes)
				networkUsage.ReduceReceived(receivedBytes)
			}

			deviceTags := GetCustomTags()
			colinkPubkey, ok := deviceTags["colink_pubkey"]
			if ok && len(colinkPubkey) > 0 {
				tags := map[string]string{
					"colink_pubkey": colinkPubkey,
				}
				//nolint: contextcheck //context is checked in the parent goroutine
				_, err = reqClient.AddDeviceTags(deviceInfo.GetName(), tags)
				if err != nil {
					log.Errorf("failed to update device tags: %v", err)
				}
			}

			time.Sleep(config.HeartbeatInterval)
		}
	}
}
