package config

import (
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"strings"

	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/coscene-io/x/conf"
	log "github.com/sirupsen/logrus"
)

const (
	LocalFilePrefix  = "file://"
	RemoteFilePrefix = "cos://"
)

type ConfManager struct {
	cfg     string
	storage *storage.Storage
}

func InitConfManager(cfg string, s *storage.Storage) *ConfManager {
	return &ConfManager{
		cfg:     cfg,
		storage: s,
	}
}

func (c ConfManager) GetStorage() *storage.Storage {
	return c.storage
}

func (c ConfManager) SetRemote(key string, value string) {
	err := (*c.storage).Put([]byte(constant.DeviceRemoteConfigBucket), []byte(key), []byte(value))
	if err != nil {
		log.Errorf("unable to set remote config: %v", err)
	}
}

func (c ConfManager) getDefaultConfig() AppConfig {
	return AppConfig{
		Collector: CollectorConfig{
			DeleteAfterIntervalInHours: 48,
			SkipCheckSameFile:          false,
		},
		Device: DeviceConfig{
			ExtraFiles: make([]string, 0),
		},
		Topics: make([]string, 0),
		Import: []string{},
	}
}

func (c ConfManager) LoadOnce() AppConfig {
	appConf := c.getDefaultConfig()

	if err := conf.ParseYAML(c.cfg, &appConf); err != nil {
		log.Fatalf("unable to load cos config: %v", err)
	}
	for _, f := range appConf.Import {
		if strings.HasPrefix(f, RemoteFilePrefix) {
			continue
		}

		localPath := strings.TrimPrefix(f, LocalFilePrefix)
		if !utils.CheckReadPath(localPath) {
			log.Warnf("local config file %s not exist", localPath)
			continue
		}

		if err := conf.ParseYAML(localPath, &appConf); err != nil {
			log.Fatalf("unable to load cos config: %v", err)
		}
	}
	return appConf
}

func (c ConfManager) LoadWithRemote() *AppConfig {
	appConf := c.LoadOnce()
	k := koanf.New(".") // Create a new koanf instance.

	for _, f := range appConf.Import {
		if strings.HasPrefix(f, RemoteFilePrefix) {
			name := strings.TrimPrefix(f, RemoteFilePrefix)
			remoteCache, err := (*c.storage).Get([]byte(constant.DeviceRemoteConfigBucket), []byte(name))

			if err != nil {
				log.Errorf("unable to get remote config: %v", err)
				continue
			}
			if remoteCache == nil || len(remoteCache) == 0 {
				log.Errorf("remote config is empty")
				continue
			}

			if err := k.Load(rawbytes.Provider(remoteCache), json.Parser()); err != nil {
				log.Errorf("unable to load remote config: %v", err)
				continue
			}
		} else {
			localPath := strings.TrimPrefix(f, LocalFilePrefix)
			if !utils.CheckReadPath(localPath) {
				log.Warnf("file %s not exist or has no permission", localPath)
				continue
			}

			if err := k.Load(file.Provider(localPath), yaml.Parser()); err != nil {
				log.Errorf("unable to load local config: %v", err)
				continue
			}
		}
	}

	err := k.Unmarshal("", &appConf)
	if err != nil {
		log.Errorf("unable to unmarshal koanf: %v", err)
	}
	return &appConf
}
