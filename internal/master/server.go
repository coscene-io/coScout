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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	log "github.com/sirupsen/logrus"
)

// Server master server.
type Server struct {
	registry *SlaveRegistry
	config   *config.MasterConfig
	server   *http.Server
	port     int
}

// NewServer creates a new master server.
func NewServer(port int, masterConfig *config.MasterConfig) *Server {
	registry := NewSlaveRegistry()

	mux := http.NewServeMux()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s := &Server{
		registry: registry,
		config:   masterConfig,
		server:   server,
		port:     port,
	}

	// Register routes
	mux.HandleFunc("/api/v1/slave/register", s.handleSlaveRegister)
	mux.HandleFunc("/api/v1/slave/heartbeat", s.handleSlaveHeartbeat)
	mux.HandleFunc("/api/v1/slave/unregister", s.handleSlaveUnregister)
	mux.HandleFunc("/api/v1/slaves", s.handleListSlaves)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	return s
}

// Start starts the server.
func (s *Server) Start(ctx context.Context) error {
	// Start cleanup goroutine
	go s.cleanupRoutine(ctx)

	log.Infof("Master server starting on port %d", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Master server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Info("Master server shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// GetRegistry returns the slave registry.
func (s *Server) GetRegistry() *SlaveRegistry {
	return s.registry
}

// Handle slave registration.
func (s *Server) handleSlaveRegister(w http.ResponseWriter, r *http.Request) {
	log.Infof("Received slave registration request from %s", r.RemoteAddr)

	if r.Method != http.MethodPost {
		log.Warnf("Invalid method for slave registration: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Errorf("Failed to decode registration request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Infof("Registration request: SlaveID=%s, Port=%d, Version=%s, FilePrefix=%s",
		req.SlaveID, req.Port, req.Version, req.FilePrefix)

	// If IP is empty, get from request
	if req.IP == "" {
		req.IP = s.getClientIP(r)
	}

	// Validate request
	if req.SlaveID == "" || req.Port <= 0 {
		http.Error(w, "Invalid slave ID or port", http.StatusBadRequest)
		return
	}

	// Check slave count limit (0 means unlimited)
	if s.config.MaxSlaves > 0 {
		onlineSlaves := s.registry.GetOnlineSlaves()
		if len(onlineSlaves) >= s.config.MaxSlaves {
			resp := RegisterResponse{
				Success: false,
				Message: "Maximum number of slaves reached",
			}
			s.writeJSON(w, resp)
			return
		}
	}

	// Register slave
	slave := &SlaveInfo{
		ID:           req.SlaveID,
		IP:           req.IP,
		Port:         req.Port,
		Version:      req.Version,
		Capabilities: req.Capabilities,
		FilePrefix:   req.FilePrefix,
	}

	if err := s.registry.Register(slave); err != nil {
		log.Errorf("Failed to register slave %s: %v", req.SlaveID, err)
		resp := RegisterResponse{
			Success: false,
			Message: err.Error(),
		}
		s.writeJSON(w, resp)
		return
	}

	log.Infof("Slave %s registered from %s:%d", req.SlaveID, req.IP, req.Port)

	resp := RegisterResponse{
		Success:  true,
		Message:  "Slave registered successfully",
		MasterID: "master-001", // Can be configurable
	}

	s.writeJSON(w, resp)
}

// Handle slave heartbeat.
func (s *Server) handleSlaveHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SlaveID == "" {
		http.Error(w, "Invalid slave ID", http.StatusBadRequest)
		return
	}

	// Check if slave exists in registry
	_, exists := s.registry.GetSlave(req.SlaveID)
	if !exists {
		// Slave not found, likely master restarted and lost registry
		log.Warnf("Received heartbeat from unregistered slave %s, requesting re-registration", req.SlaveID)
		resp := HeartbeatResponse{
			Success:        true,
			NeedReRegister: true,
			Message:        "Slave not found in registry, please re-register",
		}
		s.writeJSON(w, resp)
		return
	}

	// Update heartbeat for existing slave
	s.registry.UpdateHeartbeat(req.SlaveID)
	log.Debugf("Received heartbeat from slave %s", req.SlaveID)

	resp := HeartbeatResponse{
		Success: true,
	}

	s.writeJSON(w, resp)
}

// Handle slave unregistration.
func (s *Server) handleSlaveUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slaveID := r.URL.Query().Get("slave_id")
	if slaveID == "" {
		http.Error(w, "Missing slave_id", http.StatusBadRequest)
		return
	}

	s.registry.Unregister(slaveID)
	log.Infof("Slave %s unregistered", slaveID)

	resp := map[string]interface{}{
		"success": true,
		"message": "Slave unregistered successfully",
	}

	s.writeJSON(w, resp)
}

// Handle get slave list.
func (s *Server) handleListSlaves(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slaves := s.registry.GetOnlineSlaves()

	resp := map[string]interface{}{
		"slaves": slaves,
		"count":  len(slaves),
	}

	s.writeJSON(w, resp)
}

// Cleanup goroutine, periodically check timed-out slaves.
func (s *Server) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timeoutSlaves := s.registry.CheckAndCleanup(s.config.SlaveTimeout)
			for _, slaveID := range timeoutSlaves {
				log.Warnf("Slave %s marked as timeout", slaveID)
			}
		}
	}
}

// Get client IP.
func (s *Server) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Get from RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

// Write JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Errorf("Failed to encode JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Handle health check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status":      "ok",
		"port":        s.port,
		"slave_count": s.registry.GetSlaveCount(),
		"timestamp":   time.Now().Unix(),
	}

	s.writeJSON(w, response)
}
