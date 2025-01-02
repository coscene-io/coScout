package core

import (
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
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
