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
	"syscall"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/slave"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func NewSlaveCommand() *cobra.Command {
	var (
		port       int
		masterIP   string
		masterPort int
		slaveID    string
		filePrefix string
		ip         string
	)

	cmd := &cobra.Command{
		Use:   "slave",
		Short: "Run coScout as a slave node",
		Long: `Run coScout as a slave node that connects to a master node.
The slave node will:
- Register with the master node
- Respond to file scan requests from master
- Serve file downloads to master
- Send periodic heartbeats to maintain connection`,
		Run: func(cmd *cobra.Command, args []string) {
			// Create slave configuration
			slaveConfig := config.DefaultSlaveConfig()

			slaveConfig.IP = ip
			slaveConfig.Port = port
			slaveConfig.MasterIP = masterIP
			slaveConfig.MasterPort = masterPort
			slaveConfig.FilePrefix = filePrefix
			if slaveID != "" {
				slaveConfig.ID = slaveID
			}
			if slaveConfig.IP == "" {
				log.Fatal("Please set the IP address in the configuration file or environment variable")
			}
			if slaveConfig.FilePrefix == "" {
				slaveConfig.FilePrefix = slaveConfig.IP
			}

			// Validate required parameters
			if masterIP == "" {
				log.Fatal("Master IP address is required. Use --master-ip flag")
			}

			log.Infof("Starting slave node on port %d, connecting to master %s:%d", port, masterIP, masterPort)

			// Create context
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Create slave server
			server := slave.NewServer(slaveConfig.Port, slaveConfig.FilePrefix)

			// Create slave client
			client := slave.NewClient(slaveConfig)

			// Start slave server
			go func() {
				if err := server.Start(ctx); err != nil {
					log.Errorf("Slave server failed: %v", err)
					cancel()
				}
			}()

			// Register to master and start heartbeat with retry logic
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					default:
						if err := client.RegisterAndStartHeartbeat(ctx); err != nil {
							log.Errorf("Failed to register with master: %v", err)
							log.Info("Will retry registration in 30 seconds...")

							// Wait 30 seconds before retrying, but respect context cancellation
							select {
							case <-ctx.Done():
								return
							case <-time.After(30 * time.Second):
								log.Info("Retrying registration with master...")
								continue
							}
						} else {
							// Registration successful, heartbeat will continue in background
							log.Info("Successfully registered with master, heartbeat started")
							return
						}
					}
				}
			}()

			// Wait for signal
			shutdownChan := make(chan os.Signal, 1)
			signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

			select {
			case <-shutdownChan:
				log.Info("Slave shutdown initiated...")
			case <-ctx.Done():
				log.Info("Slave context cancelled...")
			}

			// Graceful shutdown: unregister first, then stop service
			log.Info("Unregistering from master...")
			if err := client.Unregister(context.Background()); err != nil {
				log.Errorf("Failed to unregister from master: %v", err)
			}

			log.Info("Slave stopped")
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 22525, "Port to listen on")
	cmd.Flags().StringVarP(&masterIP, "master-ip", "m", "", "Master IP address (required)")
	cmd.Flags().IntVar(&masterPort, "master-port", 22525, "Master port (default: 22525)")
	cmd.Flags().StringVar(&slaveID, "slave-id", "", "Slave ID (auto-generated if not provided)")
	cmd.Flags().StringVar(&filePrefix, "file-prefix", "", "File folder prefix for uploaded files (e.g., 'device1' creates 'device1/filename.log')")
	cmd.Flags().StringVar(&ip, "ip", "", "IP address of this slave (required)")
	return cmd
}
