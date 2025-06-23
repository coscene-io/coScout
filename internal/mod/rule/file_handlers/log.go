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

package file_handlers

import (
	"os"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/log_reader"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type logHandler struct {
	defaultGetFileSize
}

func (h *logHandler) IsFinished(filePath string) bool {
	// log files are typically not "finished" in the sense that they can be appended to.
	// However, we can assume that if the file exists and is not empty, it is "finished" for our purposes.
	return true
}

func NewLogHandler() Interface {
	return &logHandler{}
}

// CheckFilePath checks if the file path is supported by the handler.
func (h *logHandler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .log extension.
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".log")
}

func (h *logHandler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	logFileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, errors.Errorf("open log file [%s] failed: %v", filePath, err)
	}
	defer logFileReader.Close()

	reader, err := log_reader.NewLogReader(logFileReader, filePath)
	if err != nil {
		return nil, nil, errors.Errorf("failed to create log reader for log file %s: %v", filePath, err)
	}

	startTime, err := reader.GetStartTimestamp()
	if err != nil {
		return nil, nil, errors.Errorf("failed to get start timestamp for log file %s: %v", filePath, err)
	}

	endTime, err := reader.GetEndTimestamp()
	if err != nil {
		return nil, nil, errors.Errorf("failed to get end timestamp for log file %s: %v", filePath, err)
	}

	return startTime, endTime, nil
}

func (h *logHandler) SendRuleItems(filePath string, activeTopics mapset.Set[string], ruleItemChan chan rule_engine.RuleItem) {
	if activeTopics.Cardinality() > 0 && !activeTopics.Contains("/external_log") {
		return
	}

	logFileReader, err := os.Open(filePath)
	if err != nil {
		log.Errorf("open log file [%s] failed: %v", filePath, err)
		return
	}
	defer logFileReader.Close()

	reader, err := log_reader.NewLogReader(logFileReader, filePath)
	if err != nil {
		log.Errorf("failed to create log reader for log file %s: %v", filePath, err)
		return
	}

	iter, err := log_reader.NewLogIterator(reader)
	if err != nil {
		log.Errorf("failed to create log iterator for log file %s: %v", filePath, err)
		return
	}

	for {
		stampedLog, hasNext := iter.Next()
		if !hasNext {
			log.Infof("finished sending rule items for log file %s", filePath)
			break
		}

		sec := stampedLog.Timestamp.Unix()
		nsec := stampedLog.Timestamp.Nanosecond()
		tsFloat := float64(sec) + float64(nsec)/1e9

		ruleItemChan <- rule_engine.RuleItem{
			Msg: map[string]interface{}{
				"timestamp": map[string]interface{}{
					"sec":  sec,
					"nsec": nsec,
				},
				"message": stampedLog.Line,
				"file":    filePath,
				"level":   getLogLevel(stampedLog.Line),
			},
			Topic:  "/external_log",
			Ts:     tsFloat,
			Source: filePath,
		}
	}
}

func getLogLevel(line string) int {
	switch {
	case strings.Contains(line, "DEBUG"):
		return 1
	case strings.Contains(line, "WARN"):
		return 3
	case strings.Contains(line, "ERROR"):
		return 4
	case strings.Contains(line, "FATAL"):
		return 5
	default:
		return 2
	}
}
