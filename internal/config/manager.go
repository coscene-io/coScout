package config

import (
	"strings"

	"github.com/coscene-io/cos-agent/internal/storage"
	"github.com/coscene-io/cos-agent/pkg/utils"
	"github.com/coscene-io/x/conf"
	log "github.com/sirupsen/logrus"
)

const (
	LocalFilePrefix = "file://"
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

func (c ConfManager) LoadOnce() *AppConfig {
	appConf := AppConfig{}

	if err := conf.ParseYAML(c.cfg, &appConf); err != nil {
		log.Fatalf("unable to load cos config: %v", err)
	}

	for _, f := range appConf.ConfManager.Import {
		if !strings.HasPrefix(f, LocalFilePrefix) {
			continue
		}

		localPath := strings.TrimPrefix(f, LocalFilePrefix)
		if !utils.PathExist(localPath) {
			log.Warnf("file %s not exist", localPath)
		}

		if err := conf.ParseYAML(localPath, &appConf); err != nil {
			log.Fatalf("unable to load cos config: %v", err)
		}
	}
	return &appConf
}
