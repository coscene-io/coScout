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
	"path"
	"runtime"
	"strconv"

	"github.com/coscene-io/coscout"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	var (
		cfgPath  = ""
		logLevel = ""
	)

	cmd := &cobra.Command{
		Use: "cos",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, args)
		},
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			level, err := log.ParseLevel(logLevel)
			if err != nil {
				log.Fatal(err)
			}

			log.SetLevel(level)
			log.SetReportCaller(true)
			log.SetFormatter(&log.TextFormatter{
				FullTimestamp: true,
				CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
					fileName := path.Base(frame.File)
					return strconv.Itoa(frame.Line), fileName
				},
			})
		},
		Version: coscout.GetVersion(),
	}

	cmd.PersistentFlags().StringVarP(&cfgPath, "config-path", "c", "$HOME/.config/cos/config.yaml", "config path")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set the logging level, one of: debug|info|warn|error")

	cmd.AddCommand(NewVersionCommand())
	cmd.AddCommand(NewDaemonCommand(&cfgPath))
	return cmd
}
