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
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/ThreeDotsLabs/watermill"
	gcmessage "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type RulesRequest struct {
	Messages []rule_engine.RuleItem `json:"messages"`
}

type ActiveTopicsResponse struct {
	Topics []string `json:"topics"`
}

func RulesHandler(pubSub *gochannel.GoChannel) func(w http.ResponseWriter, r *http.Request) {
	limiter := rate.NewLimiter(3, 10)

	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		// Parse request
		req := RulesRequest{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Errorf("Failed to decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(req.Messages) == 0 {
			http.Error(w, "No messages in request", http.StatusBadRequest)
			return
		}

		log.Infof("Received %d messages from colistener", len(req.Messages))
		// Publish messages

		slices.SortFunc(req.Messages, func(a, b rule_engine.RuleItem) int {
			if a.Ts < b.Ts {
				return -1
			} else if a.Ts > b.Ts {
				return 1
			}
			return 0
		})

		for _, message := range req.Messages {
			message.Source = "http"
			data, err := json.Marshal(message)
			if err != nil {
				log.Errorf("Failed to marshal message: %v", err)
				continue
			}

			msg := gcmessage.NewMessage(watermill.NewUUID(), data)
			if err := pubSub.Publish(constant.TopicRuleMsg, msg); err != nil {
				log.Errorf("Failed to publish message: %v", err)
				continue
			}

			log.Debugf("Received request message: %s", string(data))
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

func ActiveTopicsHandler(ctx context.Context, pubSub *gochannel.GoChannel) func(w http.ResponseWriter, r *http.Request) {
	activeTopics := mapset.NewSet[string]()
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
				activeTopics = mapset.NewSet(topics...)
			}
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		res := ActiveTopicsResponse{
			Topics: activeTopics.ToSlice(),
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
