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

package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/collector"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/mod"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
)

const checkInterval = 60 * time.Second

func Run(confManager *config.ConfManager, reqClient *api.RequestClient, startChan chan bool, finishChan chan bool, errorChan chan error) {
	<-startChan

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := collector.Collect(ctx, reqClient, confManager, errorChan)
		if err != nil {
			errorChan <- err
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				//nolint: contextcheck // context is checked in the parent goroutine
				refreshRemoteConfig(confManager, reqClient)
			case <-ctx.Done():
				log.Infof("Daemon context done")
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

	go mod.NewModHandler(*reqClient, *confManager, errorChan, constant.TaskModType).Run(ctx)
	go mod.NewModHandler(*reqClient, *confManager, errorChan, constant.RuleModType).Run(ctx)
	go core.SendHeartbeat(ctx, reqClient, confManager.GetStorage(), errorChan)

	<-finishChan
}

func refreshRemoteConfig(confManager *config.ConfManager, reqClient *api.RequestClient) {
	appConfig := confManager.LoadOnce()
	if len(appConfig.Import) == 0 {
		return
	}

	for _, f := range appConfig.Import {
		if !strings.HasPrefix(f, config.RemoteFilePrefix) {
			continue
		}

		name := strings.TrimPrefix(f, config.RemoteFilePrefix)
		remoteCache, err := reqClient.GetConfigMapWithCache(name)
		if err != nil {
			log.Errorf("unable to get remote config: %v", err)
			continue
		}

		if remoteCache == nil || remoteCache.GetValue() == nil {
			log.Errorf("remote config is empty")
			continue
		}

		value, err := protojson.Marshal(remoteCache.GetValue())
		if err != nil {
			log.Errorf("unable to marshal remote config: %v", err)
			continue
		}

		confManager.SetRemote(name, string(value))
	}
	log.Infof("Remote config refreshed")
}
