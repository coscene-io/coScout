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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Server is the slave server that handles requests from the master.
type Server struct {
	server     *http.Server
	port       int
	filePrefix string
}

// NewServer creates a new slave server.
func NewServer(port int, filePrefix string) *Server {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  10 * time.Second,
	}

	s := &Server{
		server:     server,
		port:       port,
		filePrefix: filePrefix,
	}

	mux.HandleFunc("/api/v1/files/scan", s.handleFileScan)
	mux.HandleFunc("/api/v1/files/download", s.handleFileDownload)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	return s
}

// Start starts the slave server.
func (s *Server) Start(ctx context.Context) error {
	log.Infof("Slave server starting on port %d", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Slave server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Info("Slave server shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleFileScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req master.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Infof("Received file scan request: %+v", req)

	files := s.scanFiles(req.ScanFolders, req.AdditionalFiles, req.StartTime, req.EndTime)
	resp := master.TaskResponse{
		TaskID:  req.TaskID,
		Files:   files,
		Success: true,
	}

	s.writeJSON(w, resp)
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req master.FileTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Infof("Received file download request for %s", req.FilePath)

	realPath, err := filepath.EvalSymlinks(req.FilePath)
	if err != nil {
		http.Error(w, "File not found: "+req.FilePath, http.StatusNotFound)
		return
	}

	file, err := os.Open(realPath)
	if err != nil {
		http.Error(w, "Could not open file: "+realPath, http.StatusInternalServerError)
		return
	}
	defer file.Close()

	var reader io.Reader = file
	if req.MaxSize > 0 {
		reader = io.LimitReader(file, req.MaxSize)
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, reader); err != nil {
		log.Errorf("Failed to stream file: %v", err)
	}
}

func (s *Server) scanFiles(scanFolders []string, additionalFiles []string, startTime time.Time, endTime time.Time) []master.SlaveFileInfo {
	files, noPermissionFiles := upload.ComputeUploadFiles(scanFolders, additionalFiles, startTime, endTime)
	if len(noPermissionFiles) > 0 {
		log.Warnf("Some files/folders are not readable: %v", noPermissionFiles)
	}

	result := make([]master.SlaveFileInfo, 0)
	for _, file := range files {
		file.FileName = filepath.Join(s.filePrefix, file.FileName)
		info := master.SlaveFileInfo{
			FileInfo: file,
		}
		result = append(result, info)
	}
	return result
}

func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Errorf("Failed to encode JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
