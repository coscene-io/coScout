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

package commands

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/daemon"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/register"
	"github.com/coscene-io/coscout/internal/storage"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type authState struct {
	lifecycle atomic.Int32
	cancelMu  sync.Mutex
	cancel    context.CancelFunc
}

const (
	daemonIdle int32 = iota
	daemonRunning
	daemonStopping
)

func (s *authState) tryStart() (context.Context, bool) {
	ctx, cancel := context.WithCancel(context.Background())

	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if !s.lifecycle.CompareAndSwap(daemonIdle, daemonRunning) {
		cancel()
		return nil, false
	}
	s.cancel = cancel
	return ctx, true
}

func (s *authState) requestStop() bool {
	if !s.lifecycle.CompareAndSwap(daemonRunning, daemonStopping) {
		return false
	}

	s.cancelMu.Lock()
	cancel := s.cancel
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (s *authState) daemonStopped() {
	s.cancelMu.Lock()
	cancelRunAndMarkIdle(s.cancel, &s.lifecycle)
	s.cancel = nil
	s.cancelMu.Unlock()
}

func cancelRunAndMarkIdle(cancel context.CancelFunc, lifecycle *atomic.Int32) {
	if cancel != nil {
		cancel()
	}
	lifecycle.Store(daemonIdle)
}

func NewDaemonCommand(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run coScout as a daemon",
		Run: func(cmd *cobra.Command, args []string) {
			storageDB := storage.NewBoltDB(config.GetDBPath())
			confManager := config.InitConfManager(*cfgPath, &storageDB)

			appConf, err := confManager.LoadStartup()
			if err != nil {
				log.Fatalf("Unable to load config file from %s: %v", *cfgPath, err)
			}
			log.Infof("Load config file from %s", *cfgPath)

			registerChan := make(chan model.DeviceStatusResponse, 10)
			networkChan := make(chan *model.NetworkUsage, 100)
			reqClient := api.NewRequestClient(appConf.Api, storageDB, networkChan, registerChan)

			reg := register.NewRegister(*reqClient, appConf, storageDB)
			go reg.CheckOrRegisterDevice(registerChan)

			shutdownChan := make(chan os.Signal, 1)
			signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

			go run(confManager, reqClient, registerChan)

			<-shutdownChan
			log.Info("Daemon shutdown initiated, stopping...")
		},
	}

	return cmd
}

func run(confManager *config.ConfManager, reqClient *api.RequestClient, registerChan chan model.DeviceStatusResponse) {
	errorChan := make(chan error, 100)

	state := &authState{}
	for deviceStatus := range registerChan {
		if deviceStatus.Authorized {
			startDaemon(state, confManager, reqClient, errorChan)
			continue
		}

		stopDaemon(state)
	}
}

func startDaemon(
	state *authState,
	confManager *config.ConfManager,
	reqClient *api.RequestClient,
	errorChan chan error,
) {
	log.Info("Device is authorized. Performing actions...")
	ctx, started := state.tryStart()
	if !started {
		return
	}

	go func() {
		defer state.daemonStopped()
		if err := daemon.Run(ctx, confManager, reqClient, errorChan); err != nil {
			log.Errorf("Daemon stopped: %v", err)
		}
	}()
}

func stopDaemon(state *authState) {
	log.Warn("Device is not authorized, waiting...")
	state.requestStop()
}
