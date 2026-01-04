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

package config

import (
	"time"
)

// SlaveConfig slave configuration.
type SlaveConfig struct {
	ID                string        `yaml:"id"`                 // Unique slave ID (auto-generated)
	IP                string        `yaml:"ip"`                 // IP address of the slave, required
	Port              int           `yaml:"port"`               // Listening port
	MasterIP          string        `yaml:"master_ip"`          // Master IP address, required
	MasterPort        int           `yaml:"master_port"`        // Master port
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"` // Heartbeat interval
	RequestTimeout    time.Duration `yaml:"request_timeout"`    // Request timeout
	MaxFileSize       int64         `yaml:"max_file_size"`      // Maximum file size
	FilePrefix        string        `yaml:"file_prefix"`        // File folder prefix
}

// DefaultSlaveConfig returns default slave configuration.
func DefaultSlaveConfig() *SlaveConfig {
	return &SlaveConfig{
		IP:                "", // This must be set by the user
		Port:              22525,
		MasterIP:          "", // This must be set by the user
		MasterPort:        22525,
		HeartbeatInterval: 3 * time.Second,
		RequestTimeout:    5 * time.Second,
		MaxFileSize:       100 * 1024 * 1024 * 1024, // 100GB
		FilePrefix:        "",                       // Default: no folder prefix
	}
}

// MasterConfig master configuration.
type MasterConfig struct {
	Port              int           `yaml:"port"`                // Master listening port
	SlaveTimeout      time.Duration `yaml:"slave_timeout"`       // Slave timeout duration
	MaxSlaves         int           `yaml:"max_slaves"`          // Maximum number of slaves (0 = unlimited)
	FileTransferChunk int           `yaml:"file_transfer_chunk"` // File transfer chunk size
	RequestTimeout    time.Duration `yaml:"request_timeout"`     // Request timeout duration
}

// DefaultMasterConfig returns default master configuration.
func DefaultMasterConfig() *MasterConfig {
	return &MasterConfig{
		Port:              22525,
		SlaveTimeout:      5 * time.Second,
		MaxSlaves:         0,                // 0 means unlimited
		FileTransferChunk: 10 * 1024 * 1024, // 10MB
		RequestTimeout:    60 * time.Second,
	}
}
