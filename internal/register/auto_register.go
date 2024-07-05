package register

import (
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
)

type AutoModRegister struct {
}

func NewAutoModRegister(conf interface{}) ModRegister {
	return &AutoModRegister{}
}

func (a *AutoModRegister) GetDevice() *openDpsV1alpha1Resource.Device {
	// TODO: Implement this method
	return &openDpsV1alpha1Resource.Device{
		Name:         "devices/b3017b8b-2549-45cb-951d-bc4e0ff7ac3f",
		SerialNumber: "auto-device-serial",
	}
}
