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

	"github.com/bmatcuk/doublestar/v4"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Server is the slave server that handles requests from the master.
type Server struct {
	server     *http.Server
	port       int
	filePrefix string
	// collectFileStateHandler caches file metadata and content time
	collectFileStateHandler file_state_handler.FileStateHandler
}

// NewServer creates a new slave server.
func NewServer(port int, filePrefix string) *Server {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s := &Server{
		server:     server,
		port:       port,
		filePrefix: filePrefix,
	}

	// Initialize collect file state handler for content-based scans
	if fsh, err := file_state_handler.New("slaveCollectFile.state.json"); err != nil {
		log.Errorf("Failed to initialize file state handler: %v", err)
	} else {
		s.collectFileStateHandler = fsh
	}

	mux.HandleFunc("/api/v1/files/scan", s.handleFileScan)
	mux.HandleFunc("/api/v1/files/scan-by-content", s.handleFileScanByContent)
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
	if !utils.CheckReadPath(req.FilePath) {
		http.Error(w, "File no permission: "+req.FilePath, http.StatusBadRequest)
		return
	}

	realPath, err := filepath.EvalSymlinks(req.FilePath)
	if err != nil {
		http.Error(w, "File not found: "+req.FilePath, http.StatusNotFound)
		return
	}

	if !utils.CheckReadPath(realPath) {
		http.Error(w, "File no permission: "+req.FilePath, http.StatusBadRequest)
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

// handleFileScanByContent handles file scan requests based on file content time.
func (s *Server) handleFileScanByContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req master.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Infof("Received file scan by content request: %+v", req)
	files := s.scanFilesByContent(req.ScanFolders, req.AdditionalFiles, req.WhiteList, req.RecursivelyWalkDirs, req.StartTime, req.EndTime)
	resp := master.TaskResponse{
		TaskID:  req.TaskID,
		Files:   files,
		Success: true,
	}

	s.writeJSON(w, resp)
}

// scanFilesByContent scans files based on their content time rather than modification time.
func (s *Server) scanFilesByContent(scanFolders []string, additionalFiles []string, whiteList []string, recursivelyWalkDirs bool, startTime time.Time, endTime time.Time) []master.SlaveFileInfo {
	result := make([]master.SlaveFileInfo, 0)

	if s.collectFileStateHandler == nil {
		log.Errorf("collect file state handler is nil")
		return result
	}

	// Update cache via file_state_handler using provided scan folders
	conf := config.DefaultModConfConfig{
		CollectDirs:         scanFolders,
		RecursivelyWalkDirs: recursivelyWalkDirs,
	}
	if err := s.collectFileStateHandler.UpdateCollectDirs(whiteList, conf); err != nil {
		log.Errorf("file state handler update collect dirs: %v", err)
		return result
	}

	// Build filters: collecting + time overlap
	uploadWhiteListFilter := func(filename string, _ file_state_handler.FileState) bool {
		if len(whiteList) == 0 {
			return true
		}
		for _, pattern := range whiteList {
			matched, err := doublestar.PathMatch(pattern, filename)
			if err == nil && matched {
				return true
			}
		}
		return false
	}
	filters := []file_state_handler.FileFilter{
		file_state_handler.FilterIsCollecting(),
		file_state_handler.FilterTime(startTime.Unix(), endTime.Unix()),
		uploadWhiteListFilter,
	}
	fileStates := s.collectFileStateHandler.Files(filters...)
	log.Infof("Scanning %d files in fileStates", len(fileStates))

	for _, extraFileRaw := range additionalFiles {
		extraFileAbs, err := filepath.Abs(extraFileRaw)
		if err != nil {
			log.Errorf("get abs path for extra file: %v", err)
			continue
		}

		if !utils.CheckReadPath(extraFileAbs) {
			log.Warnf("Path %s is not readable, skip!", extraFileAbs)
			continue
		}

		realPath, fileInfo, err := utils.GetRealFileInfo(extraFileAbs)
		if err != nil {
			log.Errorf("get real file info for extra file: %v", err)
			continue
		}

		fileStates = append(fileStates, file_state_handler.FileState{
			Size:     fileInfo.Size(),
			IsDir:    fileInfo.IsDir(),
			Pathname: realPath,
		})
	}

	localFiles := upload.ComputeRuleFileInfos(fileStates)
	for _, file := range localFiles {
		fileName := filepath.Join(s.filePrefix, file.FileName)
		result = append(result, master.SlaveFileInfo{
			FileInfo: model.FileInfo{
				Path:     file.Path,
				Size:     file.Size,
				Sha256:   file.Sha256,
				FileName: fileName,
			},
		})
	}

	log.Infof("Found %d files matching content time range", len(result))
	return result
}
