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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Static errors for better error handling.
var (
	ErrSlaveRequestFailed = errors.New("slave request failed")
	ErrSlaveHealthFailed  = errors.New("slave health check failed")
)

// Client master to slave client.
type Client struct {
	httpClient *http.Client
	config     *config.MasterConfig
}

// NewClient creates a new master client.
func NewClient(masterConfig *config.MasterConfig) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: masterConfig.RequestTimeout,
		},
		config: masterConfig,
	}
}

// RequestSlaveFiles requests slave to scan files.
func (c *Client) RequestSlaveFiles(ctx context.Context, slave *SlaveInfo, req *TaskRequest) (*TaskResponse, error) {
	url := slave.GetAddr() + "/api/v1/files/scan"

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request to slave %s: %w", slave.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Wrapf(ErrSlaveRequestFailed, "slave %s returned status %d: %s", slave.ID, resp.StatusCode, string(body))
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("decode response from slave %s: %w", slave.ID, err)
	}

	// Set slave ID for all files
	for i := range taskResp.Files {
		taskResp.Files[i].SlaveID = slave.ID
	}

	return &taskResp, nil
}

// RequestSlaveFilesByContent requests slave to scan files based on content time.
func (c *Client) RequestSlaveFilesByContent(ctx context.Context, slave *SlaveInfo, req *TaskRequest) (*TaskResponse, error) {
	url := slave.GetAddr() + "/api/v1/files/scan-by-content"

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request to slave %s: %w", slave.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Wrapf(ErrSlaveRequestFailed, "slave %s returned status %d: %s", slave.ID, resp.StatusCode, string(body))
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, fmt.Errorf("decode response from slave %s: %w", slave.ID, err)
	}

	// Set slave ID for all files
	for i := range taskResp.Files {
		taskResp.Files[i].SlaveID = slave.ID
	}

	return &taskResp, nil
}

// DownloadSlaveFile downloads file from slave.
func (c *Client) DownloadSlaveFile(ctx context.Context, slave *SlaveInfo, filePath string) (io.ReadCloser, error) {
	return c.DownloadSlaveFileWithSize(ctx, slave, filePath, 0)
}

// DownloadSlaveFileWithSize downloads file from slave with specific size limit.
func (c *Client) DownloadSlaveFileWithSize(ctx context.Context, slave *SlaveInfo, filePath string, maxSize int64) (io.ReadCloser, error) {
	url := slave.GetAddr() + "/api/v1/files/download"

	req := FileTransferRequest{
		FilePath: filePath,
		MaxSize:  maxSize,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send download request to slave %s: %w", slave.ID, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Wrapf(ErrSlaveRequestFailed, "slave %s returned status %d: %s", slave.ID, resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// PingSlaveHealth checks slave health status.
func (c *Client) PingSlaveHealth(ctx context.Context, slave *SlaveInfo) error {
	url := slave.GetAddr() + "/api/v1/health"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ping slave %s: %w", slave.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Wrapf(ErrSlaveHealthFailed, "slave %s status %d", slave.ID, resp.StatusCode)
	}

	return nil
}

// RequestAllSlaveFiles concurrently requests file info from all online slaves.
func (c *Client) RequestAllSlaveFiles(ctx context.Context, registry *SlaveRegistry, req *TaskRequest) map[string]*TaskResponse {
	slaves := registry.GetOnlineSlaves()
	if len(slaves) == 0 {
		return nil
	}

	// Create result channel
	type result struct {
		slaveID string
		resp    *TaskResponse
		err     error
	}

	resultChan := make(chan result, len(slaves))

	// Concurrently request all slaves
	for _, slave := range slaves {
		go func(s *SlaveInfo) {
			// Create timeout context for each slave
			slaveCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
			defer cancel()

			resp, err := c.RequestSlaveFiles(slaveCtx, s, req)
			resultChan <- result{
				slaveID: s.ID,
				resp:    resp,
				err:     err,
			}
		}(slave)
	}

	// Collect results
	results := make(map[string]*TaskResponse)
	for range slaves {
		select {
		case res := <-resultChan:
			if res.err != nil {
				log.Errorf("Failed to get files from slave %s: %v", res.slaveID, res.err)
				// Mark slave as problematic
				if slave, exists := registry.GetSlave(res.slaveID); exists {
					slave.Status = "error"
				}
			} else {
				results[res.slaveID] = res.resp
				log.Infof("Successfully got %d files from slave %s", len(res.resp.Files), res.slaveID)
			}
		case <-ctx.Done():
			log.Warn("Context cancelled while waiting for slave responses")
			return results
		}
	}

	return results
}

// RequestAllSlaveFilesByContent concurrently requests file info from all online slaves based on content time.
func (c *Client) RequestAllSlaveFilesByContent(ctx context.Context, registry *SlaveRegistry, req *TaskRequest) map[string]*TaskResponse {
	slaves := registry.GetOnlineSlaves()
	if len(slaves) == 0 {
		return nil
	}

	// Create result channel
	type result struct {
		slaveID string
		resp    *TaskResponse
		err     error
	}

	resultChan := make(chan result, len(slaves))

	// Concurrently request all slaves
	for _, slave := range slaves {
		go func(s *SlaveInfo) {
			// Create timeout context for each slave
			slaveCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
			defer cancel()

			resp, err := c.RequestSlaveFilesByContent(slaveCtx, s, req)
			resultChan <- result{
				slaveID: s.ID,
				resp:    resp,
				err:     err,
			}
		}(slave)
	}

	// Collect results
	results := make(map[string]*TaskResponse)
	for range slaves {
		select {
		case res := <-resultChan:
			if res.err != nil {
				log.Errorf("Failed to get files from slave %s: %v", res.slaveID, res.err)
				// Mark slave as problematic
				if slave, exists := registry.GetSlave(res.slaveID); exists {
					slave.Status = "error"
				}
			} else {
				results[res.slaveID] = res.resp
				log.Infof("Successfully got %d files from slave %s", len(res.resp.Files), res.slaveID)
			}
		case <-ctx.Done():
			log.Warn("Context cancelled while waiting for slave responses")
			return results
		}
	}

	return results
}
