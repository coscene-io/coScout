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

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/djherbis/times"
	log "github.com/sirupsen/logrus"
)

// Server slave server
type Server struct {
	config *config.SlaveConfig
	server *http.Server
}

// NewServer creates a new slave server
func NewServer(slaveConfig *config.SlaveConfig) *Server {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", slaveConfig.Port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s := &Server{
		config: slaveConfig,
		server: server,
	}

	// Register routes
	mux.HandleFunc("/api/v1/files/scan", s.handleFileScan)
	mux.HandleFunc("/api/v1/files/download", s.handleFileDownload)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	return s
}

// Start starts the slave server
func (s *Server) Start(ctx context.Context) error {
	log.Infof("Slave server starting on port %d", s.config.Port)
	
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("Slave server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Info("Slave server shutting down...")
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return s.server.Shutdown(shutdownCtx)
}

// Handle file scan request
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

	log.Infof("Received file scan request for task %s", req.TaskID)

	// Scan files
	files := make([]master.SlaveFileInfo, 0)
	noPermissionFolders := make([]string, 0)

	// Scan specified folders
	for _, folder := range req.ScanFolders {
		folderFiles, noPermFolders := s.scanFolder(folder, req.StartTime, req.EndTime)
		files = append(files, folderFiles...)
		noPermissionFolders = append(noPermissionFolders, noPermFolders...)
	}

	// Handle additional files
	for _, file := range req.AdditionalFiles {
		if !utils.CheckReadPath(file) {
			log.Warnf("Path %s is not readable, skip!", file)
			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		info, err := os.Stat(file)
		if err != nil {
			log.Errorf("Failed to get file info: %v", err)
			continue
		}

		if info.IsDir() {
			folderFiles, noPermFolders := s.scanFolder(file, req.StartTime, req.EndTime)
			files = append(files, folderFiles...)
			noPermissionFolders = append(noPermissionFolders, noPermFolders...)
		} else {
			files = append(files, master.SlaveFileInfo{
				FileInfo: model.FileInfo{
					FileName: filepath.Base(file),
					Size:     info.Size(),
					Path:     file,
				},
			})
		}
	}

	resp := master.TaskResponse{
		TaskID:  req.TaskID,
		Files:   files,
		Success: true,
	}

	if len(noPermissionFolders) > 0 {
		log.Warnf("Some folders had permission issues: %v", noPermissionFolders)
	}

	s.writeJSON(w, resp)
	log.Infof("File scan completed for task %s, found %d files", req.TaskID, len(files))
}

// Handle file download request
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

	if req.FilePath == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	// Check file permissions and existence
	if !utils.CheckReadPath(req.FilePath) {
		http.Error(w, "File not accessible", http.StatusForbidden)
		return
	}

	// Check file size
	info, err := os.Stat(req.FilePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if info.Size() > s.config.MaxFileSize {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Open file
	file, err := os.Open(req.FilePath)
	if err != nil {
		log.Errorf("Failed to open file %s: %v", req.FilePath, err)
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Determine actual transfer size
	transferSize := info.Size()
	if req.MaxSize > 0 && req.MaxSize < info.Size() {
		transferSize = req.MaxSize
		log.Infof("Limiting file transfer to %d bytes (original size: %d)", transferSize, info.Size())
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(req.FilePath)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", transferSize))

	// If there's an offset, seek to specified position (resume transfer)
	if req.Offset > 0 {
		_, err := file.Seek(req.Offset, 0)
		if err != nil {
			log.Errorf("Failed to seek file %s: %v", req.FilePath, err)
			http.Error(w, "Failed to seek file", http.StatusInternalServerError)
			return
		}
	}

	// Stream file transfer with size limit
	log.Infof("Starting file transfer: %s (transfer size: %d bytes, original size: %d bytes)", 
		req.FilePath, transferSize, info.Size())
		
	if req.MaxSize > 0 && req.MaxSize < info.Size() {
		// Use limited reader for size-restricted transfer
		limitedReader := io.LimitReader(file, req.MaxSize)
		io.Copy(w, limitedReader)
	} else {
		// Use standard transfer for full file
		http.ServeContent(w, r, filepath.Base(req.FilePath), info.ModTime(), file)
	}

	log.Infof("File transfer completed: %s", req.FilePath)
}

// Handle health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0", // Can be obtained from config
	}

	s.writeJSON(w, resp)
}

// Scan folder
func (s *Server) scanFolder(folder string, startTime, endTime time.Time) ([]master.SlaveFileInfo, []string) {
	files := make([]master.SlaveFileInfo, 0)
	noPermissionFolders := make([]string, 0)

	if !utils.CheckReadPath(folder) {
		log.Warnf("Path %s is not readable, skip!", folder)
		noPermissionFolders = append(noPermissionFolders, folder)
		return files, noPermissionFolders
	}

	info, err := os.Stat(folder)
	if err != nil {
		log.Errorf("Failed to get folder info: %v", err)
		return files, noPermissionFolders
	}

	if !info.IsDir() {
		// Single file
		files = append(files, master.SlaveFileInfo{
			FileInfo: model.FileInfo{
				FileName: filepath.Base(folder),
				Size:     info.Size(),
				Path:     folder,
			},
		})
		return files, noPermissionFolders
	}

	// Scan files in directory
	filePaths, err := utils.GetAllFilePaths(folder, &utils.SymWalkOptions{
		FollowSymlinks:       true,
		SkipPermissionErrors: true,
		SkipEmptyFiles:       true,
		MaxFiles:             99999,
	})
	if err != nil {
		log.Errorf("Failed to get all file paths in folder %s: %v", folder, err)
		return files, noPermissionFolders
	}

	for _, path := range filePaths {
		if !utils.CheckReadPath(path) {
			log.Warnf("Path %s is not readable, skip!", path)
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			log.Errorf("Failed to get file info for %s: %v", path, err)
			continue
		}

		// Check file modification time
		if s.isFileInTimeRange(path, info, startTime, endTime) {
			filename, err := filepath.Rel(folder, path)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(path)
			}

			files = append(files, master.SlaveFileInfo{
				FileInfo: model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     path,
				},
			})
		}
	}

	return files, noPermissionFolders
}

// Check if file is within time range
func (s *Server) isFileInTimeRange(path string, info os.FileInfo, startTime, endTime time.Time) bool {
	// Check modification time
	if info.ModTime().After(startTime) && info.ModTime().Before(endTime) {
		return true
	}

	// If birth time is supported, also check creation time
	if stat, err := times.Stat(path); err == nil && stat.HasBirthTime() {
		if stat.BirthTime().After(startTime) && stat.BirthTime().Before(endTime) {
			return true
		}
	}

	return false
}

// Write JSON response
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Errorf("Failed to encode JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
