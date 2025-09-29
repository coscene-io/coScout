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

package server

import (
	"encoding/json"
	"net/http"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	log "github.com/sirupsen/logrus"
)

type DeviceInfoResponse struct {
	SerialNumber string `json:"serial_number"`
}

func DeviceInfoHandler(confManager config.ConfManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storage := confManager.GetStorage()
		deviceInfo := core.GetDeviceInfo(storage)
		if deviceInfo == nil || deviceInfo.GetName() == "" {
			log.Warn("device info not found")

			http.Error(w, "Device info not found", http.StatusNotFound)
			return
		}

		response := DeviceInfoResponse{
			SerialNumber: deviceInfo.GetSerialNumber(),
		}
		bytes, err := json.Marshal(response)
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			return
		}
	}
}
