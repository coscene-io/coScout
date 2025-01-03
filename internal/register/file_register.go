package register

import (
	openDpsV1alpha1Resource "buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"encoding/json"
	"fmt"
	"github.com/coscene-io/coscout"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
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

func (f *FileModRegister) GetDevice() *openDpsV1alpha1Resource.Device {
	if f.conf.SnFile == "" {
		return nil
	}

	if !utils.CheckReadPath(f.conf.SnFile) {
		log.Warnf("local device file %s not exist", f.conf.SnFile)
		return nil
	}

	// if yaml or json file, read the specific field from the file and return the device
	if strings.HasSuffix(f.conf.SnFile, ".yaml") ||
		strings.HasSuffix(f.conf.SnFile, ".yml") ||
		strings.HasSuffix(f.conf.SnFile, ".json") {
		deviceID, err := getDeviceFromStructuredFile(f.conf.SnFile, f.conf.SnField)
		if err != nil {
			log.Errorf("failed to get device from structured file: %v", err)
			return nil
		}

		return &openDpsV1alpha1Resource.Device{
			DisplayName:  deviceID,
			SerialNumber: deviceID,
			Description:  deviceID,
			Tags:         getCustomTags(),
		}
	}

	// other files, just read the first line and return the device
	deviceID, err := getDeviceFromText(f.conf.SnFile)
	if err != nil {
		log.Errorf("failed to get device from text file: %v", err)
		return nil
	}
	return &openDpsV1alpha1Resource.Device{
		DisplayName:  deviceID,
		SerialNumber: deviceID,
		Description:  deviceID,
		Tags:         getCustomTags(),
	}
}

func getDeviceFromStructuredFile(snFile, snField string) (string, error) {
	data, err := os.ReadFile(snFile)
	if err != nil {
		return "", fmt.Errorf("failed to read device file: %v", err)
	}

	var result map[string]interface{}
	if strings.HasSuffix(snFile, ".json") {
		err = json.Unmarshal(data, &result)
	} else { // yaml file
		err = yaml.Unmarshal(data, &result)
	}

	if err != nil {
		return "", fmt.Errorf("failed to parse device file: %v", err)
	}

	deviceID, ok := result[snField].(string)
	if !ok {
		return "", fmt.Errorf("field not found or not a string")
	}

	return deviceID, nil
}

func getDeviceFromText(snFile string) (string, error) {
	data, err := os.ReadFile(snFile)
	if err != nil {
		return "", fmt.Errorf("failed to read device file: %v", err)
	}

	return string(data), nil
}

func getCustomTags() map[string]string {
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
