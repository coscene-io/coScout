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

package http

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/mod/http/server"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

type CustomHttpHandler struct {
	reqClient   api.RequestClient
	confManager config.ConfManager
	errChan     chan error
	pubSub      *gochannel.GoChannel
}

func NewHttpHandler(reqClient api.RequestClient, confManager config.ConfManager, pubSub *gochannel.GoChannel, errChan chan error) *CustomHttpHandler {
	return &CustomHttpHandler{
		reqClient:   reqClient,
		confManager: confManager,
		errChan:     errChan,
		pubSub:      pubSub,
	}
}

func (c *CustomHttpHandler) Run(ctx context.Context) {
	serverPort := c.confManager.LoadWithRemote().HttpServer.Port

	router := mux.NewRouter()
	router.HandleFunc("/ruleEngine/messages", server.RulesHandler(c.pubSub)).Methods("POST")
	router.HandleFunc("/ruleEngine/activeTopics", server.ActiveTopicsHandler(ctx, c.pubSub)).Methods("GET")
	router.HandleFunc("/config/current", server.CurrentConfigHandler(c.confManager)).Methods("GET")
	router.HandleFunc("/config/setLogLevel", server.LogConfigHandler()).Methods("POST")
	router.HandleFunc("/config/setUploadStatus", server.UploadConfigHandler(c.confManager)).Methods("POST")
	router.HandleFunc("/config/getUploadStatus", server.GetUploadConfigHandler(c.confManager)).Methods("GET")

	srv := &http.Server{
		Addr:         "127.0.0.1:" + strconv.Itoa(serverPort),
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Errorf("Start HTTP server error: %v", err)
			c.errChan <- err
			return
		}
	}()

	log.Infof("HTTP server started at port %d", serverPort)
	<-ctx.Done()

	ct, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer func() {
		cancel()
	}()
	if err := srv.Shutdown(ct); err != nil {
		log.Errorf("HTTP server shutdown error: %v", err)
	}
	log.Infof("HTTP server stopped")
}
