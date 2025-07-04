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
	"os"
	"strings"

	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/utils"
	"google.golang.org/protobuf/encoding/protojson"
)

func GetDeviceInfo(storage *storage.Storage) *openDpsV1alpha1Resource.Device {
	bytes, err := (*storage).Get([]byte(constant.DeviceMetadataBucket), []byte(constant.DeviceInfoKey))
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

func GetCustomTags() map[string]string {
	tags := make(map[string]string)

	cosVersion := coscout.GetVersion()
	if cosVersion != "" {
		tags["cos_version"] = cosVersion
	}

	// check coLink
	keyPath := "/etc/colink.pub"
	if utils.CheckReadPath(keyPath) {
		data, err := os.ReadFile(keyPath)
		if err == nil {
			s := string(data)
			s = strings.TrimPrefix(s, "colink")
			s = strings.TrimSpace(s)
			tags["colink_pubkey"] = s
		}
	}

	// check virmesh
	keyPath = "/etc/virmesh.pub"
	if utils.CheckReadPath(keyPath) {
		data, err := os.ReadFile(keyPath)
		if err == nil {
			s := string(data)
			s = strings.TrimPrefix(s, "virmesh")
			s = strings.TrimSpace(s)
			tags["virmesh_pubkey"] = s
		}
	}
	return tags
}
