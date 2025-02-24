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
	"io"
	"os"
	"strings"
	"time"

	"github.com/coscene-io/coscout/pkg/mcap_ros2"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/coscene-io/coscout/pkg/utils"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/foxglove/go-rosbag/ros1msg"
	"github.com/foxglove/mcap/go/mcap"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type mcapHandler struct {
	defaultGetFileSize
}

func NewMcapHandler() Interface {
	return &mcapHandler{}
}

// CheckFilePath checks if the file path is supported by the handler.
func (h *mcapHandler) CheckFilePath(filePath string) bool {
	// Check if file exists and has .mcap extension
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && strings.HasSuffix(filePath, ".mcap")
}

func (h *mcapHandler) GetStartTimeEndTime(filePath string) (*time.Time, *time.Time, error) {
	mcapFileReader, err := os.Open(filePath)
	if err != nil {
		return nil, nil, errors.Errorf("open mcap file [%s] failed: %v", filePath, err)
	}
	defer mcapFileReader.Close()

	reader, err := mcap.NewReader(mcapFileReader)
	if err != nil {
		return nil, nil, errors.Errorf("failed to create mcap reader for mcap file %s: %v", filePath, err)
	}

	info, err := reader.Info()
	if err != nil {
		return nil, nil, errors.Errorf("failed to get info for mcap file %s: %v", filePath, err)
	}

	startNano := info.Statistics.MessageStartTime
	endNano := info.Statistics.MessageEndTime
	//nolint: gosec // ignore uint64 to int64 conversion
	start := time.Unix(int64(startNano/1e9), int64(startNano%1e9))
	//nolint: gosec // ignore uint64 to int64 conversion
	end := time.Unix(int64(endNano/1e9), int64(endNano%1e9))

	return &start, &end, nil
}

func (h *mcapHandler) SendRuleItems(filepath string, activeTopics mapset.Set[string], ruleItemChan chan rule_engine.RuleItem) {
	file, err := os.Open(filepath)
	if err != nil {
		log.Errorf("failed to open MCAP file [%s]: %v", filepath, err)
		return
	}
	defer file.Close()

	reader, err := mcap.NewReader(file)
	if err != nil {
		log.Errorf("failed to create MCAP reader: %v", err)
		return
	}
	defer reader.Close()

	info, err := reader.Info()
	if err != nil {
		log.Errorf("failed to get info for MCAP file [%s]: %v", filepath, err)
		return
	}

	// targetTopics will be empty if no active topics are provided
	// else it will be the list of active topics in the MCAP file
	targetTopics := mapset.NewSet[string]()
	if activeTopics.Cardinality() > 0 {
		for _, conn := range lo.Values(info.Channels) {
			if activeTopics.Contains(conn.Topic) {
				targetTopics.Add(conn.Topic)
			}
		}

		if targetTopics.Cardinality() == 0 {
			log.Infof("no active topics found in MCAP file %s", filepath)
			return
		}
	}
	log.Infof("sending rule items for MCAP file %s with topics: %v", filepath, targetTopics)

	iter, err := reader.Messages(mcap.WithTopics(targetTopics.ToSlice()))
	if err != nil {
		log.Errorf("failed to create message iterator: %v", err)
		return
	}

	// Remove msgBuf since we'll decode directly to map where possible
	msg := &mcap.Message{}
	msgReader := &bytes.Reader{}
	transcoders := make(map[uint16]*ros1msg.JSONTranscoder)
	ros2Decoders := make(map[string]mcap_ros2.DecoderFunction)
	descriptors := make(map[uint16]protoreflect.MessageDescriptor)
	errTopics := map[string]struct{}{}

	for {
		schema, channel, message, err := iter.NextInto(msg)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Infof("finished sending rule items for MCAP file %s", filepath)
			} else {
				log.Errorf("error reading message: %v", err)
			}
			return
		}

		if _, ok := errTopics[channel.Topic]; ok {
			continue
		}

		var decoded map[string]interface{}

		// Handle messages based on schema and encoding
		//nolint: nestif // reduce nesting
		if schema == nil || schema.Encoding == "" {
			switch channel.MessageEncoding {
			case "json":
				if err := json.Unmarshal(message.Data, &decoded); err != nil {
					log.Errorf("failed to unmarshal JSON: %v", err)
					continue
				}
			default:
				log.Errorf("unsupported message encoding for schema-less channel: %s", channel.MessageEncoding)
				continue
			}
		} else {
			switch schema.Encoding {
			case "ros1msg":
				transcoder, ok := transcoders[channel.SchemaID]
				if !ok {
					packageName := strings.Split(schema.Name, "/")[0]
					transcoder, err = ros1msg.NewJSONTranscoder(packageName, schema.Data)
					if err != nil {
						log.Errorf("failed to create JSON transcoder: %v", err)
						continue
					}
					transcoders[channel.SchemaID] = transcoder
				}
				// Still need buffer for ros1msg transcoding
				buf := &bytes.Buffer{}
				msgReader.Reset(message.Data)
				if err := transcoder.Transcode(buf, msgReader); err != nil {
					log.Errorf("failed to transcode: %v", err)
					continue
				}
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
					log.Errorf("failed to unmarshal transcoded data: %v", err)
					continue
				}

			case "ros2msg":
				decoder, ok := ros2Decoders[schema.Name]
				if !ok {
					dynamicDecoders, err := mcap_ros2.GenerateDynamic(schema.Name, string(schema.Data))
					if err != nil {
						log.Errorf("failed to generate dynamic schema decoder: %v", err)
						continue
					}

					for schemaName, decoder := range dynamicDecoders {
						ros2Decoders[schemaName] = decoder
					}

					var decoderOk bool
					decoder, decoderOk = dynamicDecoders[schema.Name]
					if !decoderOk {
						log.Errorf("failed to find decoder for schema: %s", schema.Name)
						continue
					}
				}

				var panicErr error
				func() {
					defer func() {
						if r := recover(); r != nil {
							errTopics[channel.Topic] = struct{}{}
							panicErr = errors.Errorf("panic occurred during message decoding: %v, skipping message on topic %s", r, channel.Topic)
						}
					}()
					decoded, err = decoder(message.Data)
				}()
				if panicErr != nil {
					log.Errorf("failed to decode message: %v", panicErr)
					continue
				}

			case "protobuf":
				messageDescriptor, ok := descriptors[channel.SchemaID]
				if !ok {
					fileDescriptorSet := &descriptorpb.FileDescriptorSet{}
					if err := proto.Unmarshal(schema.Data, fileDescriptorSet); err != nil {
						log.Errorf("failed to build file descriptor set: %v", err)
						continue
					}
					files, err := protodesc.FileOptions{}.NewFiles(fileDescriptorSet)
					if err != nil {
						log.Errorf("failed to create file descriptor: %v", err)
						continue
					}
					descriptor, err := files.FindDescriptorByName(protoreflect.FullName(schema.Name))
					if err != nil {
						log.Errorf("failed to find descriptor: %v", err)
						continue
					}
					messageDescriptor, _ = descriptor.(protoreflect.MessageDescriptor)
					descriptors[channel.SchemaID] = messageDescriptor
				}
				protoMsg := dynamicpb.NewMessage(messageDescriptor)
				if err := proto.Unmarshal(message.Data, protoMsg); err != nil {
					log.Errorf("failed to parse protobuf message: %v", err)
					continue
				}
				marshalledBytes, err := protojson.Marshal(protoMsg)
				if err != nil {
					log.Errorf("failed to marshal protobuf to JSON: %v", err)
					continue
				}
				if err := json.Unmarshal(marshalledBytes, &decoded); err != nil {
					log.Errorf("failed to unmarshal protobuf JSON: %v", err)
					continue
				}

			case "jsonschema":
				if err := json.Unmarshal(message.Data, &decoded); err != nil {
					log.Errorf("failed to unmarshal JSON: %v", err)
					continue
				}

			default:
				log.Errorf("unsupported schema encoding: %s", schema.Encoding)
				continue
			}
		}

		ruleItemChan <- rule_engine.RuleItem{
			Msg:   decoded,
			Ts:    utils.FloatSecFromTime(utils.TimeFromFloat(float64(msg.PublishTime))),
			Topic: channel.Topic,
		}
	}
}
