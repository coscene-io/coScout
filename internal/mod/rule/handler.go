package rule

import (
	"context"
	"time"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	log "github.com/sirupsen/logrus"
)

const checkInterval = 60 * time.Second

type CustomRuleHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
}

func NewRuleHandler(reqClient api.RequestClient, confManager config.ConfManager, errChan chan error) *CustomRuleHandler {
	return &CustomRuleHandler{
		reqClient:   reqClient,
		confManager: confManager,
		errChan:     errChan,
	}
}

func (c CustomRuleHandler) Run(ctx context.Context) {
	// checkChan := make(chan model.FileInfo, 1000)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				log.Infof("Checking folders")
			case <-ctx.Done():
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

	<-ctx.Done()
	log.Infof("Rule handler stopped")
}
