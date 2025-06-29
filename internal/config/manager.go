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

package config

import (
	"strings"

	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
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
		Api: ApiConfig{
			Insecure: false,
		},
		Collector: CollectorConfig{
			DeleteAfterIntervalInHours: 48,
			SkipCheckSameFile:          false,
		},
		Device: DeviceConfig{
			ExtraFiles: make([]string, 0),
		},
		Topics: make([]string, 0),
		Import: []string{},
		Mod: ModConfConfig{
			Name: "default",
			Config: DefaultModConfConfig{
				SkipPeriodHours:     2,
				RecursivelyWalkDirs: false,
			},
		},
		HttpServer: HttpServerConfig{
			Port: 22524,
		},
	}
}

func (c ConfManager) LoadOnce() AppConfig {
	appConf := c.getDefaultConfig()

	if err := utils.ParseYAML(c.cfg, &appConf); err != nil {
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

		if err := utils.ParseYAML(localPath, &appConf); err != nil {
			log.Fatalf("unable to load cos config: %v", err)
		}
	}
	return appConf
}

func (c ConfManager) LoadWithRemote() *AppConfig {
	appConf := c.LoadOnce()
	k := koanf.New(".") // Create a new koanf instance.

	for _, f := range appConf.Import {
		//nolint: nestif // no need to nest if
		if strings.HasPrefix(f, RemoteFilePrefix) {
			name := strings.TrimPrefix(f, RemoteFilePrefix)
			remoteCache, err := (*c.storage).Get([]byte(constant.DeviceRemoteConfigBucket), []byte(name))

			if err != nil {
				log.Errorf("unable to get remote config: %v", err)
				continue
			}
			//nolint: gosimple // no need to simplify
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
