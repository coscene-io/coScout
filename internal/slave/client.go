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
	"io"
	"net/http"
	"time"

	"github.com/coscene-io/coscout"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Static errors for better error handling.
var (
	ErrUnregisterFailed     = errors.New("unregister failed")
	ErrRegistrationFailed   = errors.New("registration failed")
	ErrRegistrationRejected = errors.New("master rejected registration")
	ErrHeartbeatFailed      = errors.New("heartbeat failed")
)

// Client is the slave client that communicates with the master.
type Client struct {
	config     *config.SlaveConfig
	httpClient *http.Client
	slaveID    string
	ip         string
}

// NewClient creates a new slave client.
func NewClient(cfg *config.SlaveConfig) *Client {
	slaveID := cfg.ID
	if slaveID == "" {
		slaveID = generateSlaveID()
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		slaveID: slaveID,
		ip:      cfg.IP,
	}
}

// RegisterAndStartHeartbeat registers with the master and starts sending heartbeats.
func (c *Client) RegisterAndStartHeartbeat(ctx context.Context) error {
	if err := c.register(ctx); err != nil {
		return errors.Wrap(err, "failed to register with master")
	}

	go c.startHeartbeat(ctx)

	return nil
}

// Unregister sends an unregister request to the master.
func (c *Client) Unregister(ctx context.Context) error {
	url := fmt.Sprintf("http://%s/api/v1/slave/unregister?slave_id=%s", c.config.MasterAddr, c.slaveID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create unregister request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to send unregister request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(ErrUnregisterFailed, "status %d: %s", resp.StatusCode, string(body))
	}

	log.Info("Successfully unregistered from master")
	return nil
}

func (c *Client) register(ctx context.Context) error {
	url := fmt.Sprintf("http://%s/api/v1/slave/register", c.config.MasterAddr)
	reqPayload := master.RegisterRequest{
		SlaveID:    c.slaveID,
		IP:         c.ip,
		Port:       c.config.Port,
		Version:    coscout.GetVersion(),
		FilePrefix: c.config.FilePrefix,
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal register request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return errors.Wrap(err, "failed to create register request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to send register request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return errors.Wrapf(ErrRegistrationFailed, "status %d: %s", resp.StatusCode, string(respBody))
	}

	var regResp master.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return errors.Wrap(err, "failed to decode register response")
	}

	if !regResp.Success {
		return errors.Wrapf(ErrRegistrationRejected, "message: %s", regResp.Message)
	}

	log.Infof("Successfully registered with master. Master ID: %s", regResp.MasterID)
	return nil
}

func (c *Client) startHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.sendHeartbeat(ctx); err != nil {
				log.Errorf("Failed to send heartbeat: %v", err)
				// Consider re-registration logic here
			}
		case <-ctx.Done():
			log.Info("Heartbeat stopped")
			return
		}
	}
}

func (c *Client) sendHeartbeat(ctx context.Context) error {
	url := fmt.Sprintf("http://%s/api/v1/slave/heartbeat", c.config.MasterAddr)

	reqPayload := master.HeartbeatRequest{
		SlaveID: c.slaveID,
		Status:  "online",
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal heartbeat request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return errors.Wrap(err, "failed to create heartbeat request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to send heartbeat")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrapf(ErrHeartbeatFailed, "status %d", resp.StatusCode)
	}

	log.Debug("Heartbeat sent successfully")
	return nil
}

func generateSlaveID() string {
	// Generate a new UUID and take the first 16 characters.
	return uuid.New().String()[:16]
}
