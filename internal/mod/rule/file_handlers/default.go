package file_handlers

import (
	"os"
	"strings"
	"time"

	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/djherbis/times"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type defaultHandler struct {
	defaultGetFileSize
}

func NewDefaultHandler() Interface {
	return &defaultHandler{}
}

func (h *defaultHandler) CheckFilePath(filePath string) bool {
	// Check if file exists and not bag/log/mcap extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	if !info.IsDir() && (strings.HasSuffix(filePath, ".log") ||
		strings.HasSuffix(filePath, ".bag") ||
		strings.HasSuffix(filePath, ".mcap")) {
		return false
	}

	stat, err := times.Stat(filePath)
	if err != nil {
		return false
	}

	return stat.HasBirthTime()
}

func (h *defaultHandler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	stat, err := times.Stat(filePath)
	if err != nil {
		return nil, nil, err
	}

	if !stat.HasBirthTime() {
		return nil, nil, errors.New("file has no BirthTime")
	}

	birthTime := stat.BirthTime()
	modTime := stat.ModTime()
	return &birthTime, &modTime, nil
}

func (h *defaultHandler) SendRuleItems(filePath string, activeTopics mapset.Set[string], ruleItemChan chan rule_engine.RuleItem) {
	log.Info("filePath not supported by rule engine: ", filePath, ", skip sending rule items")
}

func (h *defaultHandler) IsFinished(filePath string) bool {
	// By default, we assume files are finished.
	return true
}
