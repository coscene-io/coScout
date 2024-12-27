package collector

import (
	"context"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/model"
	log "github.com/sirupsen/logrus"
)

func Upload(ctx context.Context, reqClient *api.RequestClient, confManager *config.ConfManager, uploadChan chan *model.RecordCache, errorChan chan error) {
	for {
		select {
		case recordCache := <-uploadChan:
			log.Infof("Waiting for upload: %v", recordCache.Timestamp)
		case <-ctx.Done():
			return
		}
	}
}
