package register

import (
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"encoding/json"
	"github.com/coscene-io/coscout/internal/config"
	log "github.com/sirupsen/logrus"
)

type FileModRegister struct {
	conf config.FileModRegisterConfig
}

func NewFileModRegister(conf interface{}) ModRegister {
	bytes, err := json.Marshal(conf)
	if err != nil {
		log.Errorf("Unable to marshal mod register config: %v", err)
		return nil
	}

	reConf := config.FileModRegisterConfig{}
	if err := json.Unmarshal(bytes, &reConf); err != nil {
		log.Errorf("Unable to unmarshal mod register config: %v", err)
		return nil
	}

	return &FileModRegister{
		conf: reConf,
	}
}

func (a *FileModRegister) GetDevice() *openDpsV1alpha1Resource.Device {
	// TODO: Implement this method
	return &openDpsV1alpha1Resource.Device{
		Name:         "file-device",
		SerialNumber: "file-serial",
	}
}
