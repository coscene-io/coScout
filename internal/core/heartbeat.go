package core

import (
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/services"
	"context"
	"github.com/coscene-io/coscout"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
	"time"
)

const heartbeatInterval = 60 * time.Second

type ErrorInfo struct {
	Err       error
	Timestamp int64
}

func SendHeartbeat(ctx context.Context, reqClient *api.RequestClient, storage *storage.Storage, errChan chan error) {
	deviceInfo := GetDeviceInfo(storage)
	if deviceInfo == nil || deviceInfo.GetName() == "" {
		return
	}

	lastError := ErrorInfo{}
	go func(c chan error) {
		for {
			select {
			case err := <-c:
				lastError.Err = err
				lastError.Timestamp = time.Now().Unix()
			}
		}
	}(errChan)

	networkUsage := services.NetworkUsage{}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			extraInfo := map[string]string{}
			if lastError.Err != nil && lastError.Timestamp > time.Now().Add(-5*heartbeatInterval).Unix() {
				extraInfo["error_msg"] = lastError.Err.Error()
				extraInfo["code"] = "ERROR"
			} else {
				extraInfo["code"] = "OK"
			}

			cosVersion := coscout.GetVersion()
			_, err := reqClient.SendHeartbeat(deviceInfo.GetName(), cosVersion, &networkUsage, extraInfo)
			if err != nil {
				log.Errorf("failed to send heartbeat: %v", err)
			} else {
				log.Infof("device %s heartbeat sent", deviceInfo.GetDisplayName())
			}

			time.Sleep(heartbeatInterval)
		}
	}
}
