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
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/foxglove/go-rosbag"
	"github.com/foxglove/go-rosbag/ros1msg"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

type ros1Handler struct {
	defaultGetFileSize
}

func NewRos1Handler() Interface {
	return &ros1Handler{}
}

// CheckFilePath checks if the file path is supported by the handler.
func (h *ros1Handler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .ros1 extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".bag")
}

func (h *ros1Handler) IsFinished(filePath string) bool {
	// bag files are finished when they are closed
	// since we read them in a streaming manner, we assume they are finished
	return true
}

func (h *ros1Handler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	ros1FileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, errors.Errorf("open ros1 file [%s] failed: %v", filePath, err)
	}
	defer ros1FileReader.Close()

	reader, err := rosbag.NewReader(ros1FileReader)
	if err != nil {
		return nil, nil, errors.Errorf("failed to create ros1 reader for ros1 file %s: %v", filePath, err)
	}

	info, err := reader.Info()
	if err != nil {
		return nil, nil, errors.Errorf("failed to get info for ros1 file %s: %v", filePath, err)
	}

	startNano := info.MessageStartTime
	endNano := info.MessageEndTime
	//nolint: gosec // ignore uint64 to int64 conversion
	start := time.Unix(int64(startNano/1e9), int64(startNano%1e9))
	//nolint: gosec // ignore uint64 to int64 conversion
	end := time.Unix(int64(endNano/1e9), int64(endNano%1e9))

	return &start, &end, nil
}

func (h *ros1Handler) SendRuleItems(filePath string, activeTopics mapset.Set[string], ruleItemChan chan rule_engine.RuleItem) {
	reader, err := os.Open(filePath)
	if err != nil {
		log.Errorf("failed to open ros1 file [%s]: %v", filePath, err)
		return
	}

	// Create bag reader
	bagReader, err := rosbag.NewReader(reader)
	if err != nil {
		log.Errorf("failed to create bag reader: %v", err)
		return
	}

	info, err := bagReader.Info()
	if err != nil {
		log.Errorf("failed to get info for ros1 file %s: %v", filePath, err)
		return
	}

	// targetTopics will be empty if there is no active topic
	// else it will be the intersection of active topics and channels in the bag file
	// will read all topics if activeTopics is empty
	targetTopics := mapset.NewSet[string]()
	if activeTopics.Cardinality() > 0 {
		for _, conn := range lo.Values(info.Connections) {
			if activeTopics.Contains(conn.Topic) {
				targetTopics.Add(conn.Topic)
			}
		}

		if targetTopics.Cardinality() == 0 {
			log.Infof("no active topics matched in ros1 file %s, skipping", filePath)
			return
		}
	}
	log.Infof("sending rule items for ros1 file %s with topics: %v", filePath, targetTopics)

	// Create message iterator
	it, err := bagReader.Messages()
	if err != nil {
		log.Errorf("failed to create message iterator: %v", err)
		return
	}

	// Map to store JSON transcoders for each connection
	transcoders := make(map[uint32]*ros1msg.JSONTranscoder)

	// Set to store conn failed to transcode
	failedConnToTranscode := mapset.NewSet[uint32]()

	// Read messages
	for it.More() {
		conn, msg, err := it.Next()
		if err != nil {
			log.Errorf("failed to read message: %v", err)
			continue
		}
		if targetTopics.Cardinality() > 0 && !targetTopics.Contains(conn.Topic) {
			continue
		}

		// Skip conn failed to transcode
		if failedConnToTranscode.Contains(conn.Conn) {
			continue
		}

		// Get or create transcoder for this connection
		transcoder, exists := transcoders[conn.Conn]
		if !exists {
			parentPackage := ""
			if idx := strings.LastIndex(conn.Data.Type, "/"); idx != -1 {
				parentPackage = conn.Data.Type[:idx]
			}
			transcoder, err = ros1msg.NewJSONTranscoder(parentPackage, conn.Data.MessageDefinition)
			if err != nil {
				log.Errorf("failed to create JSON transcoder: %v", err)
				failedConnToTranscode.Add(conn.Conn)
				continue
			}
			transcoders[conn.Conn] = transcoder
		}

		// Convert message data to JSON
		var buf bytes.Buffer
		err = transcoder.Transcode(&buf, bytes.NewReader(msg.Data))
		if err != nil {
			log.Errorf("failed to transcode message: %v", err)
			continue
		}

		// Parse JSON into structured data
		var structuredData map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &structuredData); err != nil {
			log.Errorf("failed to parse JSON: %v", err)
			continue
		}

		sec, nsec := utils.NormalizeFloatTimestamp(float64(msg.Time))
		// Send JSON message through channel
		ruleItemChan <- rule_engine.RuleItem{
			Msg:    structuredData,
			Ts:     float64(sec) + float64(nsec)/1e9,
			Topic:  conn.Topic,
			Source: filePath,
		}
	}
	log.Infof("finished sending rule items for ros1 file %s", filePath)
}
