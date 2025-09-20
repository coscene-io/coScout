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
	"os"
	"os/signal"
	"runtime"
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
	isAuthed atomic.Bool
}

func NewDaemonCommand(cfgPath *string, maxProcs *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run coScout as a daemon",
		Run: func(cmd *cobra.Command, args []string) {
			log.Infof("maxProcs is set to %d", *maxProcs)
			if *maxProcs > 0 {
				runtime.GOMAXPROCS(*maxProcs)
				log.Infof("Set GOMAXPROCS to %d", *maxProcs)
			}

			storageDB := storage.NewBoltDB(config.GetDBPath())
			confManager := config.InitConfManager(*cfgPath, &storageDB)

			appConf := confManager.LoadOnce()
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
	startChan := make(chan bool, 1)
	exitChan := make(chan bool, 1)
	errorChan := make(chan error, 100)

	state := &authState{}
	state.isAuthed.Store(false)
	for deviceStatus := range registerChan {
		if deviceStatus.Authorized {
			log.Info("Device is authorized. Performing actions...")

			if !state.isAuthed.Load() {
				go daemon.Run(confManager, reqClient, startChan, exitChan, errorChan)
				startChan <- true
			}
			state.isAuthed.Store(true)
		} else {
			log.Warn("Device is not authorized, waiting...")

			if state.isAuthed.Load() {
				exitChan <- true
			}
			state.isAuthed.Store(false)
		}
	}
}
