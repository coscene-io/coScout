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
	"encoding/json"
	"net/http"

	"github.com/ThreeDotsLabs/watermill"
	gcmessage "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type RulesRequest struct {
	Messages []rule_engine.RuleItem `json:"messages"`
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

		// Publish messages
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

			log.Infof("Published topic messages: %+v", message)
		}

		bytes, err := json.Marshal(map[string]string{"status": "ok"})
		if err != nil {
			log.Errorf("Failed to marshal response: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Respond
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(bytes)
		if err != nil {
			return
		}
	}
}
