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
	"strconv"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
)

type LogConfigRequest struct {
	Level string `json:"level"`
}

type UploadStatusRequest struct {
	Disabled bool `json:"disabled"`
}

func LogConfigHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse request
		var req LogConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		level, err := log.ParseLevel(req.Level)
		if err != nil {
			http.Error(w, "Invalid log level", http.StatusBadRequest)
			return
		}

		log.SetLevel(level)

		bytes, err := json.Marshal(map[string]string{"status": "ok"})
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Respond
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			return
		}
	}
}

func CurrentConfigHandler(confManager config.ConfManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		appConfig := confManager.LoadWithRemote()

		bytes, err := json.Marshal(appConfig)
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Respond
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			log.Errorf("Failed to write response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Debugf("Current config: %s", string(bytes))
		log.Infof("Current config served successfully")
	}
}

func UploadConfigHandler(confManager config.ConfManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UploadStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		storage := confManager.GetStorage()
		if storage == nil {
			log.Error("Storage is not initialized")
			http.Error(w, "Storage is not initialized", http.StatusInternalServerError)
			return
		}

		boolStr := strconv.FormatBool(req.Disabled)
		err := (*storage).Put([]byte(constant.ConfigInfoBucket), []byte(constant.UploadStatusConfigKey), []byte(boolStr))
		if err != nil {
			log.Errorf("Failed to set upload status: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		bytes, err := json.Marshal(map[string]string{"status": "ok"})
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			log.Errorf("Failed to write response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("Upload status updated successfully, disabled: %v", req.Disabled)
	}
}

func GetUploadConfigHandler(confManager config.ConfManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		storage := confManager.GetStorage()
		if storage == nil {
			log.Error("Storage is not initialized")
			http.Error(w, "Storage is not initialized", http.StatusInternalServerError)
			return
		}

		v, err := (*storage).Get([]byte(constant.ConfigInfoBucket), []byte(constant.UploadStatusConfigKey))
		if err != nil {
			log.Errorf("Failed to get upload status: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		disabled, err := strconv.ParseBool(string(v))
		if err != nil {
			log.Errorf("Failed to parse upload status: %v", err)
			http.Error(w, "Invalid upload status value", http.StatusInternalServerError)
			return
		}

		bytes, err := json.Marshal(UploadStatusRequest{Disabled: disabled})
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			log.Errorf("Failed to write response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("Get upload status successfully")
	}
}
