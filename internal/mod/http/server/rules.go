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

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/ThreeDotsLabs/watermill"
	gcmessage "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/queuedbytes"
	"github.com/coscene-io/coscout/pkg/constant"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type ActiveTopicsResponse struct {
	Topics []string `json:"topics"`
}

type queuedPayload struct {
	data          []byte
	timestamp     float64
	reservedBytes int64
}

// queuedPayloadOverheadBytes conservatively accounts for the retained slice
// entry, RawMessage allocation, and allocator bookkeeping that len(data) does
// not include. This keeps arrays of tiny messages inside the same byte budget.
const queuedPayloadOverheadBytes int64 = 256

var (
	errRequestNotObject      = errors.New("request must be a JSON object")
	errInvalidRequestField   = errors.New("request field name must be a string")
	errMessagesNotArray      = errors.New("messages must be a JSON array")
	errUnclosedMessagesArray = errors.New("messages JSON array was not closed")
	errUnclosedRequestObject = errors.New("request JSON object was not closed")
	errMessageMustBeObject   = errors.New("msg must be a JSON object or null")
	errUnexpectedJSONDelim   = errors.New("unexpected JSON delimiter")
)

func RulesHandler(pubSub *gochannel.GoChannel, budget *queuedbytes.Budget) func(w http.ResponseWriter, r *http.Request) {
	limiter := rate.NewLimiter(3, 10)

	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		body, releaseBody, err := budget.ReserveBody(r.Body, r.ContentLength)
		if err != nil {
			rejectQueuedBytes(w, budget, r.ContentLength)
			return
		}
		defer releaseBody()

		payloads, err := decodeRuleMessages(body, budget)
		if err != nil {
			if errors.Is(err, queuedbytes.ErrLimitExceeded) {
				rejectQueuedBytes(w, budget, r.ContentLength)
				return
			}
			log.Errorf("Failed to decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(payloads) == 0 {
			http.Error(w, "No messages in request", http.StatusBadRequest)
			return
		}

		log.Infof("Received %d messages from colistener", len(payloads))
		slices.SortFunc(payloads, func(a, b queuedPayload) int {
			if a.timestamp < b.timestamp {
				return -1
			} else if a.timestamp > b.timestamp {
				return 1
			}
			return 0
		})

		defer func() {
			for _, payload := range payloads {
				budget.Release(payload.reservedBytes)
			}
		}()

		for i := range payloads {
			payload := &payloads[i]
			msg := gcmessage.NewMessage(watermill.NewUUID(), payload.data)
			if !budget.TrackMessage(msg.UUID, payload.reservedBytes) {
				log.Errorf("Failed to track queued bytes for message %s", msg.UUID)
				http.Error(w, "failed to track rule message", http.StatusInternalServerError)
				return
			}

			publishErr := pubSub.Publish(constant.TopicRuleMsg, msg)
			claimed := budget.FinishMessage(msg.UUID)
			if claimed {
				payload.reservedBytes = 0
			}

			if publishErr != nil {
				log.Errorf("Failed to publish message: %v", publishErr)
				http.Error(w, "failed to publish rule message", http.StatusServiceUnavailable)
				return
			}
			if !claimed {
				log.Errorf("Rule message %s was not claimed by a subscriber", msg.UUID)
				http.Error(w, "rule message consumer unavailable", http.StatusServiceUnavailable)
				return
			}

			log.WithField("payloadBytes", len(payload.data)).Debug("Published request message")
		}

		bytes, err := json.Marshal(map[string]string{"status": "ok"})
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Respond
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			return
		}
	}
}

func decodeRuleMessages(body io.Reader, budget *queuedbytes.Budget) ([]queuedPayload, error) {
	var payloads []queuedPayload
	success := false
	defer func() {
		if !success {
			releaseQueuedPayloads(budget, payloads)
		}
	}()

	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errRequestNotObject
	}

	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errInvalidRequestField
		}

		if !strings.EqualFold(key, "messages") {
			if skipErr := skipJSONValue(decoder); skipErr != nil {
				return nil, skipErr
			}
			continue
		}

		replacement, decodeErr := decodeMessageArray(decoder, budget)
		if decodeErr != nil {
			return nil, decodeErr
		}
		releaseQueuedPayloads(budget, payloads)
		payloads = replacement
	}

	token, err = decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return nil, errUnclosedRequestObject
	}
	success = true
	return payloads, nil
}

func decodeMessageArray(decoder *json.Decoder, budget *queuedbytes.Budget) ([]queuedPayload, error) {
	var payloads []queuedPayload
	success := false
	defer func() {
		if !success {
			releaseQueuedPayloads(budget, payloads)
		}
	}()

	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return nil, errMessagesNotArray
	}

	for decoder.More() {
		var data json.RawMessage
		if err := decoder.Decode(&data); err != nil {
			return nil, err
		}

		metadata, err := decodeRuleMessageMetadata(data)
		if err != nil {
			return nil, fmt.Errorf("decode rule message metadata: %w", err)
		}
		size := int64(len(data)) + queuedPayloadOverheadBytes
		if !budget.TryReserve(size) {
			return nil, queuedbytes.ErrLimitExceeded
		}
		payloads = append(payloads, queuedPayload{
			data:          data,
			timestamp:     metadata,
			reservedBytes: size,
		})
	}

	token, err = decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != ']' {
		return nil, errUnclosedMessagesArray
	}
	success = true
	return payloads, nil
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errUnexpectedJSONDelim
	}

	_, err = decoder.Token()
	return err
}

func releaseQueuedPayloads(budget *queuedbytes.Budget, payloads []queuedPayload) {
	for _, payload := range payloads {
		budget.Release(payload.reservedBytes)
	}
}

func decodeRuleMessageMetadata(data []byte) (float64, error) {
	var metadata ruleMessageMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return 0, err
	}
	return metadata.Timestamp, nil
}

type ruleMessageMetadata struct {
	Message     validatedMessageObject `json:"msg"`
	Topic       string                 `json:"topic"`
	Timestamp   float64                `json:"ts"`
	MessageType string                 `json:"msgType"`
	Source      string                 `json:"source"`
}

type validatedMessageObject struct{}

func (*validatedMessageObject) UnmarshalJSON(data []byte) error {
	message := bytes.TrimSpace(data)
	if bytes.Equal(message, []byte("null")) {
		return nil
	}
	if len(message) == 0 || message[0] != '{' {
		return errMessageMustBeObject
	}

	decoder := json.NewDecoder(bytes.NewReader(message))
	for {
		if _, err := decoder.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func rejectQueuedBytes(w http.ResponseWriter, budget *queuedbytes.Budget, contentLength int64) {
	log.WithFields(log.Fields{
		"contentLength": contentLength,
		"queuedBytes":   budget.Queued(),
		"limitBytes":    budget.Limit(),
	}).Warn("Dropping rule messages request because HTTP queued bytes limit was exceeded")
	w.Header().Set("Retry-After", "1")
	http.Error(w, queuedbytes.ErrLimitExceeded.Error(), http.StatusTooManyRequests)
}

func ActiveTopicsHandler(ctx context.Context, pubSub *gochannel.GoChannel) func(w http.ResponseWriter, r *http.Request) {
	var activeTopics atomic.Value
	activeTopics.Store([]string{})

	go func() {
		msg, err := pubSub.Subscribe(ctx, constant.TopicConfigTopicsMsg)
		if err != nil {
			log.Errorf("Failed to subscribe to topic %s: %v", constant.TopicConfigTopicsMsg, err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				log.Infof("Stopping active topics subscription")
				return
			case m := <-msg:
				if m == nil {
					log.Warnf("Received nil message from topic %s", constant.TopicConfigTopicsMsg)
					continue
				}

				m.Ack()
				var topics []string
				if err := json.Unmarshal(m.Payload, &topics); err != nil {
					log.Errorf("Failed to unmarshal topics from message: %v", err)
					continue
				}
				log.Infof("Received active topics: %v", topics)
				activeTopics.Store(topics)
			}
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		topics, ok := activeTopics.Load().([]string)
		if !ok {
			log.Errorf("Unexpected active topics value type")
			topics = []string{}
		}
		res := ActiveTopicsResponse{
			Topics: topics,
		}
		bytes, err := json.Marshal(res)
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Respond
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			return
		}
	}
}
