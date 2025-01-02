package daemon

import (
	"context"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/collector"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/core"
	"github.com/coscene-io/coscout/internal/mod/task"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"strings"
	"time"
)

const checkInterval = 60 * time.Second

func Run(confManager *config.ConfManager, reqClient *api.RequestClient, startChan chan bool, finishChan chan bool, errorChan chan error) {
	<-startChan

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // This will signal all goroutines to exit gracefully

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
				refreshRemoteConfig(confManager, reqClient)
				appConfig := confManager.LoadWithRemote()

				err := task.NewTaskHandler(*reqClient, *appConfig, confManager.GetStorage()).Run()
				if err != nil {
					errorChan <- err
				}
			case <-ctx.Done():
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

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
}
