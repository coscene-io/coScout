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

package slave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// Client slave to master client
type Client struct {
	httpClient *http.Client
	config     *config.SlaveConfig
	masterAddr string
	slaveID    string
}

// NewClient creates a new slave client
func NewClient(slaveConfig *config.SlaveConfig) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: slaveConfig.RequestTimeout,
		},
		config:     slaveConfig,
		masterAddr: slaveConfig.MasterAddr,
		slaveID:    slaveConfig.ID,
	}
}

// formatMasterURL formats the master address to support both IPv4 and IPv6
func (c *Client) formatMasterURL(path string) string {
	// Handle the case where address already includes brackets for IPv6
	if strings.HasPrefix(c.masterAddr, "[") {
		// Address like [::1]:22525 or [fd07:b51a:cc66:f0::fe]:22525
		return fmt.Sprintf("http://%s%s", c.masterAddr, path)
	}
	
	host, port, err := net.SplitHostPort(c.masterAddr)
	if err != nil {
		// No port specified, need to check if it's IPv6 without port
		if net.ParseIP(c.masterAddr) != nil && strings.Contains(c.masterAddr, ":") {
			// IPv6 without port, add default port with brackets
			return fmt.Sprintf("http://[%s]:22525%s", c.masterAddr, path)
		}
		// IPv4 or hostname without port
		return fmt.Sprintf("http://%s:22525%s", c.masterAddr, path)
	}
	
	// Check if host is an IPv6 address
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		// IPv6 address needs brackets
		return fmt.Sprintf("http://[%s]:%s%s", host, port, path)
	}
	
	// IPv4 address or hostname
	return fmt.Sprintf("http://%s:%s%s", host, port, path)
}

// Register registers to master
func (c *Client) Register(ctx context.Context) error {
	if c.slaveID == "" {
		c.slaveID = c.generateSlaveID()
		log.Infof("Generated slave ID: %s", c.slaveID)
	}

	url := c.formatMasterURL("/api/v1/slave/register")
	
	req := master.RegisterRequest{
		SlaveID:      c.slaveID,
		Port:         c.config.Port,
		Version:      "1.0.0", // Can be obtained from config
		Capabilities: []string{"file_scan", "file_download"},
		FilePrefix:   c.config.FilePrefix,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal register request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("create register request: %w", err)
	}
	
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send register request: %w", err)
	}
	defer resp.Body.Close()

	var registerResp master.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}

	if !registerResp.Success {
		return fmt.Errorf("registration failed: %s", registerResp.Message)
	}

	log.Infof("Successfully registered with master %s", registerResp.MasterID)
	return nil
}

// Unregister unregisters from master
func (c *Client) Unregister(ctx context.Context) error {
	url := c.formatMasterURL(fmt.Sprintf("/api/v1/slave/unregister?slave_id=%s", c.slaveID))
	
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create unregister request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send unregister request: %w", err)
	}
	defer resp.Body.Close()

	log.Infof("Successfully unregistered from master")
	return nil
}

// SendHeartbeat sends heartbeat
func (c *Client) SendHeartbeat(ctx context.Context) error {
	url := c.formatMasterURL("/api/v1/slave/heartbeat")
	
	req := master.HeartbeatRequest{
		SlaveID:      c.slaveID,
		Version:      "1.0.0",
		Capabilities: []string{"file_scan", "file_download"},
		Status:       "online",
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal heartbeat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}
	
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	var heartbeatResp master.HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&heartbeatResp); err != nil {
		return fmt.Errorf("decode heartbeat response: %w", err)
	}

	if !heartbeatResp.Success {
		return fmt.Errorf("heartbeat failed: %s", heartbeatResp.Message)
	}

	log.Debugf("Heartbeat sent successfully")
	return nil
}

// StartHeartbeat starts heartbeat goroutine
func (c *Client) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	// Send heartbeat immediately
	if err := c.SendHeartbeat(ctx); err != nil {
		log.Errorf("Initial heartbeat failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("Heartbeat goroutine stopped")
			return
		case <-ticker.C:
			if err := c.SendHeartbeat(ctx); err != nil {
				log.Errorf("Heartbeat failed: %v", err)
				// Auto re-register on connection failures
				if c.isConnectionError(err) {
					log.Warn("Master appears to be unreachable, attempting auto re-registration...")
					c.retryRegistration(ctx)
				}
			}
		}
	}
}

// RegisterAndStartHeartbeat registers and starts heartbeat
func (c *Client) RegisterAndStartHeartbeat(ctx context.Context) error {
	// Initial registration
	if err := c.Register(ctx); err != nil {
		return fmt.Errorf("initial registration failed: %w", err)
	}

	// Start heartbeat goroutine
	go c.StartHeartbeat(ctx)

	return nil
}

// GetSlaveID returns slave ID
func (c *Client) GetSlaveID() string {
	return c.slaveID
}

// generateSlaveID generates fixed-length slave ID (16 characters, conforming to Linux device naming convention)
func (c *Client) generateSlaveID() string {
	// Use first 16 characters of UUID (remove hyphens) to ensure uniqueness and fixed length
	uuidStr := strings.ReplaceAll(uuid.New().String(), "-", "")
	return uuidStr[:16] // Fixed 16 character length
}

// isConnectionError checks if the error indicates a connection problem
func (c *Client) isConnectionError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "no route to host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "EOF")
}

// retryRegistration attempts to re-register with exponential backoff
func (c *Client) retryRegistration(ctx context.Context) {
	maxRetries := 10
	baseDelay := time.Second
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			log.Info("Registration retry cancelled due to context cancellation")
			return
		default:
		}
		
		// Exponential backoff with jitter
		delay := time.Duration(attempt) * baseDelay
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		
		log.Infof("Re-registration attempt %d/%d (waiting %v)...", attempt, maxRetries, delay)
		time.Sleep(delay)
		
		if err := c.Register(ctx); err != nil {
			log.Errorf("Re-registration attempt %d failed: %v", attempt, err)
			if attempt == maxRetries {
				log.Error("Max re-registration attempts reached, giving up")
				return
			}
		} else {
			log.Info("Successfully re-registered with master after connection recovery")
			return
		}
	}
}
