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
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type DeleteMultiUploadPartsRequest struct {
	AbsPath string `json:"absPath"`
}

func MultiUploadPartsHandler(confManager config.ConfManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DeleteMultiUploadPartsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if !utils.CheckReadPath(req.AbsPath) {
			log.Warnf("Invalid path: %s", req.AbsPath)
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		storage := confManager.GetStorage()
		if storage == nil {
			log.Error("Storage is not initialized")
			http.Error(w, "Storage is not initialized", http.StatusInternalServerError)
			return
		}

		uploadIdKey := upload.GetUploadIdKey(req.AbsPath)
		uploadedSizeKey := upload.GetUploadedSizeKey(req.AbsPath)
		partsKey := upload.GetUploadPartsKey(req.AbsPath)

		err := (*storage).Delete([]byte(constant.MultiPartUploadBucket), []byte(uploadIdKey))
		if err != nil {
			log.Errorf("Delete upload id failed: %v", err)
		}
		err = (*storage).Delete([]byte(constant.MultiPartUploadBucket), []byte(partsKey))
		if err != nil {
			log.Errorf("Delete parts failed: %v", err)
		}
		err = (*storage).Delete([]byte(constant.MultiPartUploadBucket), []byte(uploadedSizeKey))
		if err != nil {
			log.Errorf("Delete uploaded size failed: %v", err)
		}

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
