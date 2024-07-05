package register

import (
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/cos-agent/internal/config"
	log "github.com/sirupsen/logrus"
)

type AgiModRegister struct {
	conf config.AgiModRegisterConfig
}

func NewAgiModRegister(conf interface{}) ModRegister {
	registerConfig, ok := conf.(config.AgiModRegisterConfig)
	if !ok {
		log.Errorf("Invalid config type: %T", conf)
		return nil
	}
	return &AgiModRegister{
		conf: registerConfig,
	}
}

func (a *AgiModRegister) GetDevice() *openDpsV1alpha1Resource.Device {
	// TODO: Implement this method
	return &openDpsV1alpha1Resource.Device{
		Name: "agi-device",
	}
}
