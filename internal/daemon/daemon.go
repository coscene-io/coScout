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

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/collector"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/mod"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
)

func Run(confManager *config.ConfManager, reqClient *api.RequestClient, startChan chan bool, finishChan chan bool, errorChan chan error) {
	<-startChan

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			Persistent:                     false,
			OutputChannelBuffer:            1000,
			BlockPublishUntilSubscriberAck: true,
		},
		watermill.NewStdLogger(false, false),
	)
	defer func(pubSub *gochannel.GoChannel) {
		err := pubSub.Close()
		if err != nil {
			log.Errorf("Unable to close pubsub: %v", err)
		}
	}(pubSub)

	go func() {
		err := collector.Collect(ctx, reqClient, confManager, pubSub, errorChan)
		if err != nil {
			select {
			case errorChan <- err:
			default:
				log.Warnf("Error channel is full, dropping error: %v", err)
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				ticker.Reset(config.RefreshRemoteConfigInterval)

				//nolint: contextcheck // context is checked in the parent goroutine
				refreshRemoteConfig(confManager, reqClient)
			case <-ctx.Done():
				log.Infof("Daemon ticker goroutine done")
				return
			}
		}
	}(ticker)

	go core.SendHeartbeat(ctx, reqClient, confManager.GetStorage(), errorChan)
	go mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.TaskModType).Run(ctx)
	go mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.RuleModType).Run(ctx)
	go mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.HttpModType).Run(ctx)

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
			log.Errorf("Unable to get remote config: %v", err)
			continue
		}

		if remoteCache == nil || remoteCache.GetValue() == nil {
			log.Errorf("Remote config is empty")
			continue
		}

		value, err := protojson.Marshal(remoteCache.GetValue())
		if err != nil {
			log.Errorf("Unable to marshal remote config: %v", err)
			continue
		}

		confManager.SetRemote(name, string(value))
	}
	log.Infof("Remote config refreshed")
}
