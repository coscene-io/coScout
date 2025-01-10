package rule

import (
	"context"
	"github.com/coscene-io/coscout/pkg/utils"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	log "github.com/sirupsen/logrus"
)

const checkInterval = 60 * time.Second

type CheckFileInfo struct {
	FileName string
	FilePath string
	Size     int64
}

type CollectCutFileInfo struct {
	extraFiles []string
	// time in seconds
	start int64
	// time in seconds
	end       int64
	whiteList []string
}

type CollectFileInfo struct {
	projectName   string
	record        map[string]interface{}
	diagnosisTask map[string]interface{}
	cut           CollectCutFileInfo
}

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
	checkChan := make(chan CheckFileInfo, 1000)
	collectChan := make(chan CollectFileInfo, 1000)
	modConfig := config.DefaultModConfConfig{SkipPeriodHours: 2}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func(t *time.Ticker) {
		for {
			select {
			case <-t.C:
				appConfig := c.confManager.LoadWithRemote()
				confConfig, ok := appConfig.Mod.Config.(config.DefaultModConfConfig)
				if ok {
					modConfig = confConfig
				}

				c.checkListenFolders(modConfig, checkChan)
			case <-ctx.Done():
				return
			}

			ticker.Reset(checkInterval)
		}
	}(ticker)

	go c.checkFilesWithRule(ctx, modConfig, checkChan, collectChan)
	go c.handleCollectFiles(ctx, collectChan)

	<-ctx.Done()
	log.Infof("Rule handler stopped")
}

func (c CustomRuleHandler) checkListenFolders(modConfig config.DefaultModConfConfig, checkResultChan chan CheckFileInfo) {
	listenDirs := modConfig.ListenDirs
	if len(listenDirs) == 0 {
		return
	}

	for _, folder := range listenDirs {
		if !utils.CheckReadPath(folder) {
			log.Warnf("Folder %s not exist", folder)
			continue
		}

		err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Errorf("Error walking through %s: %v", folder, err)
				return err
			}

			if !utils.CheckReadPath(path) {
				return nil
			}

			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(folder, path)
			if err != nil {
				log.Errorf("Error getting relative path: %v", err)
				return err
			}

			// TODO(shuhao): check file should be checked by ruleEngine
			checkResultChan <- CheckFileInfo{
				FileName: rel,
				FilePath: path,
				Size:     info.Size(),
			}
			return nil
		})

		if err != nil {
			c.errChan <- err
		}
	}
}

func (c CustomRuleHandler) checkFilesWithRule(ctx context.Context, modConfig config.DefaultModConfConfig, checkResultChan chan CheckFileInfo, collectChan chan CollectFileInfo) {
	semaphore := make(chan struct{}, 2)
	var wg sync.WaitGroup

	for {
		select {
		case checkFileInfo, ok := <-checkResultChan:
			if !ok {
				// checkResultChan 已关闭，等待所有正在进行的操作完成
				wg.Wait()
				return
			}

			// 获取信号量
			semaphore <- struct{}{}

			wg.Add(1)
			go func(info CheckFileInfo) {
				defer wg.Done()
				defer func() { <-semaphore }() // 释放信号量

				// TODO(shuhao): check file with ruleEngine
				c.validateFileWithRule(info, collectChan)
			}(checkFileInfo)
		case <-ctx.Done():
			wg.Wait()
			return
		}
	}
}

func (c CustomRuleHandler) validateFileWithRule(checkFileInfo CheckFileInfo, collectChan chan CollectFileInfo) {
	// TODO(shuhao): collect file with ruleEngine
	log.Infof("RuleEngine exec file: %v", checkFileInfo.FilePath)

	collectChan <- CollectFileInfo{
		projectName: "test",
	}
}

func (c CustomRuleHandler) handleCollectFiles(ctx context.Context, collectChan chan CollectFileInfo) {
	for {
		select {
		case collectFileInfo, ok := <-collectChan:
			if !ok {
				return
			}

			log.Infof("Collect file: %v", collectFileInfo)
			// TODO(shuhao): handle collect file
		case <-ctx.Done():
			return
		}
	}
}
