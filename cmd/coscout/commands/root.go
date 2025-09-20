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
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/coscene-io/coscout"
	internallog "github.com/coscene-io/coscout/internal/log"
	"github.com/coscene-io/coscout/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

func NewCommand() *cobra.Command {
	var (
		cfgPath   = ""
		logLevel  = ""
		logFolder = ""
		maxProcs  = 0
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
					fileName := filepath.Base(frame.File)
					return strconv.Itoa(frame.Line), fileName
				},
			})

			if len(logFolder) > 0 && utils.CheckReadPath(logFolder) {
				logFilepath := path.Join(logFolder, "cos.log")
				logDir := filepath.Dir(logFilepath)
				if err := os.MkdirAll(logDir, 0755); err != nil {
					log.Fatalf("Failed to create log directory: %v", err)
				}

				// Use lumberjack for log rotation
				lumberjackLogger := &lumberjack.Logger{
					Filename:   logFilepath,
					MaxSize:    20,
					MaxBackups: 5,
					MaxAge:     30,
					Compress:   false,
					LocalTime:  true,
				}

				// Wrap with buffer layer to reduce syscalls
				// 64KB buffer, 100ms flush interval
				bufferedWriter := internallog.NewBufferedWriter(
					lumberjackLogger,
					64*1024, // 64KB buffer
					time.Second,
				)

				log.SetOutput(bufferedWriter)
				cobra.OnFinalize(func() {
					err := bufferedWriter.Flush()
					if err != nil {
						log.Errorf("Failed to flush buffered writer: %v", err)
					}
					err = lumberjackLogger.Close()
					if err != nil {
						log.Errorf("Failed to close lumberjack logger: %v", err)
					}
				})
			}
		},
		Version: coscout.GetVersion(),
	}

	defaultConfigPath := path.Join("$HOME", ".config", "cos", "config.yaml")
	cmd.PersistentFlags().StringVarP(&cfgPath, "config-path", "c", defaultConfigPath, "config path")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set the logging level, one of: debug|info|warn|error")
	cmd.PersistentFlags().StringVarP(&logFolder, "log-dir", "l", "", "log file directory")
	cmd.PersistentFlags().IntVarP(&maxProcs, "max-procs", "p", 0, "set the maximum number of CPUs to use. Default is all available CPUs")

	cmd.AddCommand(NewVersionCommand())
	cmd.AddCommand(NewDaemonCommand(&cfgPath, &maxProcs))
	return cmd
}
