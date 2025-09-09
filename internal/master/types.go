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

package master

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coscene-io/coscout/internal/model"
)

const (
	// SlaveStatusOnline represents online status.
	SlaveStatusOnline = "online"
)

// SlaveInfo stores slave node information.
type SlaveInfo struct {
	ID           string    `json:"id"`           // Unique slave identifier
	IP           string    `json:"ip"`           // Slave IP address
	Port         int       `json:"port"`         // Slave port
	LastSeen     time.Time `json:"last_seen"`    // Last heartbeat time
	Status       string    `json:"status"`       // Status: online, offline, timeout
	Version      string    `json:"version"`      // Slave version
	Capabilities []string  `json:"capabilities"` // Features supported by slave
	FilePrefix   string    `json:"file_prefix"`  // File folder prefix
}

// IsOnline checks if slave is online.
func (s *SlaveInfo) IsOnline() bool {
	return s.Status == SlaveStatusOnline && time.Since(s.LastSeen) < 30*time.Second
}

// GetAddr returns the complete slave address with proper IPv6 support.
func (s *SlaveInfo) GetAddr() string {
	// Check if IP is an IPv6 address
	if net.ParseIP(s.IP) != nil && strings.Contains(s.IP, ":") {
		// IPv6 address needs brackets
		return "http://[" + s.IP + "]:" + strconv.Itoa(s.Port)
	}
	// IPv4 address or hostname
	return "http://" + net.JoinHostPort(s.IP, strconv.Itoa(s.Port))
}

// SlaveRegistry slave registry, thread-safe.
type SlaveRegistry struct {
	mu     sync.RWMutex
	slaves map[string]*SlaveInfo // key: slave ID
}

// NewSlaveRegistry creates a new slave registry.
func NewSlaveRegistry() *SlaveRegistry {
	return &SlaveRegistry{
		slaves: make(map[string]*SlaveInfo),
	}
}

// Register registers or updates a slave.
func (r *SlaveRegistry) Register(slave *SlaveInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slave.LastSeen = time.Now()
	slave.Status = SlaveStatusOnline
	r.slaves[slave.ID] = slave
}

// Unregister removes a slave from registry.
func (r *SlaveRegistry) Unregister(slaveID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.slaves, slaveID)
}

// GetSlave returns information for a specific slave.
func (r *SlaveRegistry) GetSlave(slaveID string) (*SlaveInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	slave, exists := r.slaves[slaveID]
	return slave, exists
}

// GetOnlineSlaves returns all online slaves.
func (r *SlaveRegistry) GetOnlineSlaves() []*SlaveInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var onlineSlaves []*SlaveInfo
	for _, slave := range r.slaves {
		if slave.IsOnline() {
			onlineSlaves = append(onlineSlaves, slave)
		}
	}
	return onlineSlaves
}

// GetSlaveCount returns the total number of registered slaves.
func (r *SlaveRegistry) GetSlaveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.slaves)
}

// UpdateHeartbeat updates slave heartbeat timestamp.
func (r *SlaveRegistry) UpdateHeartbeat(slaveID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if slave, exists := r.slaves[slaveID]; exists {
		slave.LastSeen = time.Now()
		slave.Status = SlaveStatusOnline
	}
}

// CheckAndCleanup checks and marks timed-out slaves.
func (r *SlaveRegistry) CheckAndCleanup(timeout time.Duration) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var timeoutSlaves []string
	now := time.Now()

	for id, slave := range r.slaves {
		if now.Sub(slave.LastSeen) > timeout {
			slave.Status = "timeout"
			timeoutSlaves = append(timeoutSlaves, id)
		}
	}

	return timeoutSlaves
}

// SlaveFileInfo contains slave file information.
type SlaveFileInfo struct {
	model.FileInfo
	SlaveID string `json:"slave_id"` // Source slave ID
}

// GetRemotePath returns remote file identifier (using slave:// protocol format).
func (sfi *SlaveFileInfo) GetRemotePath() string {
	return FormatSlaveFilePath(sfi.SlaveID, sfi.Path)
}

// FormatSlaveFilePath formats a slave file path using the slave:// protocol.
func FormatSlaveFilePath(slaveID, filePath string) string {
	// Ensure filePath starts with "/"
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	return fmt.Sprintf("slave://%s%s", slaveID, filePath)
}

// TaskRequest task request structure.
type TaskRequest struct {
	TaskID          string    `json:"task_id"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	ScanFolders     []string  `json:"scan_folders"`
	AdditionalFiles []string  `json:"additional_files"`
}

// TaskResponse task response structure.
type TaskResponse struct {
	TaskID  string          `json:"task_id"`
	Files   []SlaveFileInfo `json:"files"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
}

// HeartbeatRequest heartbeat request structure.
type HeartbeatRequest struct {
	SlaveID      string   `json:"slave_id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Status       string   `json:"status"`
}

// HeartbeatResponse heartbeat response structure.
type HeartbeatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// FileTransferRequest file transfer request structure.
type FileTransferRequest struct {
	FilePath  string `json:"file_path"`
	Offset    int64  `json:"offset,omitempty"`     // Resume transfer offset
	ChunkSize int    `json:"chunk_size,omitempty"` // Chunk size
	MaxSize   int64  `json:"max_size,omitempty"`   // Maximum file size to read (for append files)
}

// RegisterRequest slave registration request structure.
type RegisterRequest struct {
	SlaveID      string   `json:"slave_id"`
	IP           string   `json:"ip"`
	Port         int      `json:"port"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	FilePrefix   string   `json:"file_prefix"` // File folder prefix
}

// RegisterResponse slave registration response structure.
type RegisterResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	MasterID string `json:"master_id,omitempty"`
}
