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
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod"
	"github.com/coscene-io/coscout/internal/queuedbytes"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
)

func Run(
	ctx context.Context,
	confManager *config.ConfManager,
	reqClient *api.RequestClient,
	errorChan chan error,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if runCtx.Err() != nil {
		return nil
	}

	appConfig, err := confManager.LoadWithRemote()
	if err != nil {
		if appConfig == nil {
			return err
		}
		log.Warnf("Unable to reload config, continuing with last-known-good config: %v", err)
	}
	queuedBytesLimit := appConfig.HttpServer.QueuedBytesLimit
	if queuedBytesLimit <= 0 {
		log.Warnf(
			"Invalid HTTP queued bytes limit %d, falling back to %d",
			queuedBytesLimit,
			config.DefaultHTTPQueuedBytesLimit,
		)
		queuedBytesLimit = config.DefaultHTTPQueuedBytesLimit
	}
	ruleQueuedBytes := queuedbytes.NewBudget(queuedBytesLimit)

	// Determine mode based on configuration
	if appConfig.MasterSlave.Enabled {
		log.Info("Starting daemon in master-slave mode")
	} else {
		log.Info("Starting daemon in single-node mode")
	}

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

	// Start master server if master-slave mode is enabled
	var masterServer *master.Server
	var masterConfig *config.MasterConfig
	var fileManager *master.FileManager
	var masterResult <-chan error
	if appConfig.MasterSlave.Enabled {
		masterConfig = config.DefaultMasterConfig()
		masterServer = master.NewServer(masterConfig.Port, masterConfig)
		result := make(chan error, 1)
		masterResult = result
		go func() {
			result <- masterServer.Start(runCtx)
		}()

		select {
		case <-masterServer.Ready():
			if runCtx.Err() != nil {
				err := <-masterResult
				cancel()
				return err
			}
			log.Infof("Master server started on port %d", masterConfig.Port)
		case err := <-masterResult:
			if err != nil {
				log.Errorf("Master server failed: %v", err)
			}
			cancel()
			return err
		case <-runCtx.Done():
			err := <-masterResult
			cancel()
			return err
		}

		// Set up FileManager for slave file handling
		masterClient := master.NewClient(masterConfig)
		fileManager = master.NewFileManager(masterClient, masterServer.GetRegistry())
		log.Info("FileManager configured for slave file uploads")
	}

	// Start collector
	go func() {
		err := collector.Collect(runCtx, reqClient, confManager, fileManager, pubSub, errorChan)
		if err != nil {
			select {
			case errorChan <- err:
			default:
				log.Warnf("Error channel is full, dropping error: %v", err)
			}
		}
	}()

	// Start configuration refresh timer
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				ticker.Reset(config.RefreshRemoteConfigInterval)
				//nolint: contextcheck // context is checked in the parent goroutine
				refreshRemoteConfig(confManager, reqClient)
			case <-runCtx.Done():
				log.Infof("Daemon ticker goroutine done")
				return
			}
		}
	}(ticker)

	// Start core services
	go core.SendHeartbeat(runCtx, reqClient, confManager.GetStorage(), errorChan)

	// Start task handler with optional master-slave enhancement
	taskHandler := mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.TaskModType, ruleQueuedBytes)
	if appConfig.MasterSlave.Enabled && masterServer != nil {
		if customTaskHandler, ok := taskHandler.(interface {
			EnhanceTaskHandlerWithMasterSlave(*master.SlaveRegistry, *config.MasterConfig)
		}); ok {
			customTaskHandler.EnhanceTaskHandlerWithMasterSlave(masterServer.GetRegistry(), masterConfig)
			log.Info("Task handler enhanced with master-slave support")
		}
	}
	go taskHandler.Run(runCtx)

	// Start rule handler with optional master-slave enhancement
	ruleHandler := mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.RuleModType, ruleQueuedBytes)
	if appConfig.MasterSlave.Enabled && masterServer != nil {
		if customRuleHandler, ok := ruleHandler.(interface {
			EnhanceRuleHandlerWithMasterSlave(*master.SlaveRegistry, *config.MasterConfig)
		}); ok {
			customRuleHandler.EnhanceRuleHandlerWithMasterSlave(masterServer.GetRegistry(), masterConfig)
			log.Info("Rule handler enhanced with master-slave support")
		}
	}
	go ruleHandler.Run(runCtx)

	// Start HTTP handler
	go mod.NewModHandler(*reqClient, *confManager, pubSub, errorChan, constant.HttpModType, ruleQueuedBytes).Run(runCtx)

	log.Info("Daemon started successfully")
	if masterResult != nil {
		return waitForMasterAndCancel(runCtx, masterResult, cancel)
	}

	<-runCtx.Done()
	cancel()
	return nil
}

// waitForMasterAndCancel returns a runtime master failure immediately and
// cancels all other daemon services before returning. On daemon cancellation
// it still waits for Server.Start to finish shutdown so callers cannot start a
// replacement daemon before the listening port is released.
func waitForMasterAndCancel(
	ctx context.Context,
	masterResult <-chan error,
	cancel context.CancelFunc,
) error {
	var err error
	select {
	case err = <-masterResult:
	case <-ctx.Done():
		err = <-masterResult
	}
	cancel()
	return err
}

func refreshRemoteConfig(confManager *config.ConfManager, reqClient *api.RequestClient) {
	appConfig, err := confManager.LoadOnce()
	if err != nil {
		log.Errorf("Unable to load local config while refreshing remote config: %v", err)
		return
	}
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
