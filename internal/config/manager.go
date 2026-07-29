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
	"sync"

	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	LocalFilePrefix  = "file://"
	RemoteFilePrefix = "cos://"
)

type ConfManager struct {
	cfg     string
	storage *storage.Storage
	cache   *configCache
}

type configCache struct {
	mu       sync.RWMutex
	lastGood *AppConfig
}

func InitConfManager(cfg string, s *storage.Storage) *ConfManager {
	return &ConfManager{
		cfg:     cfg,
		storage: s,
		cache:   &configCache{},
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
			Port:             22524,
			QueuedBytesLimit: DefaultHTTPQueuedBytesLimit,
		},
	}
}

func (c ConfManager) LoadOnce() (AppConfig, error) {
	appConf := c.getDefaultConfig()

	if err := utils.ParseYAML(c.cfg, &appConf); err != nil {
		return AppConfig{}, errors.Wrap(err, "unable to load cos config")
	}
	for _, f := range appConf.Import {
		if strings.HasPrefix(f, RemoteFilePrefix) {
			continue
		}

		localPath := strings.TrimPrefix(f, LocalFilePrefix)
		if !utils.CheckReadPath(localPath) {
			return AppConfig{}, errors.Errorf("unable to load local config %s: file does not exist or is not readable", localPath)
		}

		if err := utils.ParseYAML(localPath, &appConf); err != nil {
			return AppConfig{}, errors.Wrapf(err, "unable to load local config %s", localPath)
		}
	}
	return appConf, nil
}

// LoadStartup loads the local startup configuration and seeds it as an initial
// fallback. Remote imports are intentionally not required at this stage; the
// first successful LoadWithRemote replaces this baseline with the complete
// merged configuration.
func (c ConfManager) LoadStartup() (AppConfig, error) {
	appConf, err := c.LoadOnce()
	if err != nil {
		return AppConfig{}, err
	}

	c.setLastKnownGood(appConf)
	return appConf, nil
}

func (c ConfManager) LoadWithRemote() (*AppConfig, error) {
	appConf, err := c.LoadOnce()
	if err != nil {
		return c.lastKnownGood(), err
	}
	k := koanf.New(".") // Create a new koanf instance.

	for _, f := range appConf.Import {
		//nolint: nestif // no need to nest if
		if strings.HasPrefix(f, RemoteFilePrefix) {
			name := strings.TrimPrefix(f, RemoteFilePrefix)
			if c.storage == nil || *c.storage == nil {
				return c.lastKnownGood(), errors.Errorf("unable to load remote config %s: storage is not configured", name)
			}
			remoteCache, err := (*c.storage).Get([]byte(constant.DeviceRemoteConfigBucket), []byte(name))

			if err != nil {
				return c.lastKnownGood(), errors.Wrapf(err, "unable to get remote config %s", name)
			}
			//nolint: gosimple // no need to simplify
			if remoteCache == nil || len(remoteCache) == 0 {
				return c.lastKnownGood(), errors.Errorf("unable to load remote config %s: config is empty", name)
			}

			if err := k.Load(rawbytes.Provider(remoteCache), json.Parser()); err != nil {
				return c.lastKnownGood(), errors.Wrapf(err, "unable to load remote config %s", name)
			}
		} else {
			localPath := strings.TrimPrefix(f, LocalFilePrefix)
			if !utils.CheckReadPath(localPath) {
				return c.lastKnownGood(), errors.Errorf("unable to load local config %s: file does not exist or is not readable", localPath)
			}

			if err := k.Load(file.Provider(localPath), yaml.Parser()); err != nil {
				return c.lastKnownGood(), errors.Wrapf(err, "unable to load local config %s", localPath)
			}
		}
	}

	if err := k.Unmarshal("", &appConf); err != nil {
		return c.lastKnownGood(), errors.Wrap(err, "unable to unmarshal config")
	}

	c.setLastKnownGood(appConf)
	return &appConf, nil
}

func (c ConfManager) setLastKnownGood(appConf AppConfig) {
	if c.cache == nil {
		return
	}

	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.lastGood = &appConf
}

func (c ConfManager) lastKnownGood() *AppConfig {
	if c.cache == nil {
		return nil
	}

	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	if c.cache.lastGood == nil {
		return nil
	}
	appConf := *c.cache.lastGood
	return &appConf
}
