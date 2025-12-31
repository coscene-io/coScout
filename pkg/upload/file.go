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

package upload

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/djherbis/times"
	log "github.com/sirupsen/logrus"
)

func ComputeUploadFiles(scanFolders []string, additionalFiles []string, startTime time.Time, endTime time.Time) (map[string]model.FileInfo, []string) {
	files := make(map[string]model.FileInfo)
	noPermissionFolders := make([]string, 0)

	for _, folder := range scanFolders {
		if !utils.CheckReadPath(folder) {
			log.Warnf("Path %s is not readable, skip!", folder)

			noPermissionFolders = append(noPermissionFolders, folder)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(folder)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to get all file paths in folder %s: %v", folder, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			timeStat, err := times.Stat(realPath)
			if err != nil {
				log.Errorf("Failed to get file times for %s: %v", realPath, err)
				continue
			}

			fileStartTime := info.ModTime()
			fileEndTime := info.ModTime()
			if timeStat.HasBirthTime() {
				fileStartTime = timeStat.BirthTime()
			}
			log.Infof("file %s, start time: %s, end time: %s", realPath, fileStartTime, fileEndTime)

			if fileStartTime.Unix() <= endTime.Unix() && fileEndTime.Unix() >= startTime.Unix() {
				filename, err := filepath.Rel(folder, realPath)
				if err != nil {
					log.Errorf("Failed to get relative path: %v", err)
					filename = filepath.Base(realPath)
				}

				files[realPath] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     realPath,
				}
			}
		}
	}

	for _, file := range additionalFiles {
		if !utils.CheckReadPath(file) {
			log.Warnf("Path %s is not readable, skip!", file)

			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(file)
		if err != nil {
			log.Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		// Clean the file path to handle trailing slashes correctly
		cleanFile := filepath.Clean(realPath)
		parentFolder := filepath.Dir(cleanFile)
		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("Failed to walk through folder %s: %v", realPath, err)
			continue
		}

		for _, path := range filePaths {
			if !utils.CheckReadPath(path) {
				log.Warnf("Path %s is not readable, skip!", path)
				continue
			}

			realPath, info, err := utils.GetRealFileInfo(path)
			if err != nil {
				log.Errorf("Failed to get file info for %s: %v", path, err)
				continue
			}

			if !utils.CheckReadPath(realPath) {
				log.Warnf("Path %s is not readable, skip!", realPath)
				continue
			}

			filename, err := filepath.Rel(parentFolder, realPath)
			if err != nil {
				log.Errorf("Failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			files[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
		}
	}

	return files, noPermissionFolders
}

func ComputeRuleFileInfos(fileStates []file_state_handler.FileState) map[string]model.FileInfo {
	files := make(map[string]model.FileInfo)
	for _, fileState := range fileStates {
		if !fileState.IsDir {
			realPath, info, err := utils.GetRealFileInfo(fileState.Pathname)
			if err != nil {
				log.Errorf("failed to stat file %s: %v", realPath, err)
				continue
			}

			fileName := filepath.Base(realPath)
			switch {
			case strings.HasSuffix(fileName, ".bag"):
				fileName = path.Join("bag", fileName)
			case strings.HasSuffix(fileName, ".log"):
				fileName = path.Join("log", fileName)
			case strings.HasSuffix(fileName, ".mcap"):
				fileName = path.Join("mcap", fileName)
			default:
				fileName = path.Join("files", fileName)
			}

			files[realPath] = model.FileInfo{
				FileName: fileName,
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		realPath, _, err := utils.GetRealFileInfo(fileState.Pathname)
		if err != nil {
			log.Errorf("failed to stat dir %s: %v", realPath, err)
			continue
		}
		baseDir := filepath.Dir(realPath)
		filePaths, err := utils.GetAllFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		})
		if err != nil {
			log.Errorf("failed to get all file paths: %v", err)
			continue
		}

		for _, filePath := range filePaths {
			realPath, info, err := utils.GetRealFileInfo(filePath)
			if err != nil {
				log.Errorf("failed to stat file %s: %v", realPath, err)
				continue
			}

			if info.IsDir() {
				continue
			}

			filename, err := filepath.Rel(baseDir, realPath)
			if err != nil {
				log.Errorf("failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			files[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
		}
	}

	return files
}

func GetUploadIdKey(absPath string) string {
	return fmt.Sprintf(uploadIdKeyTemplate, absPath)
}

func GetUploadedSizeKey(absPath string) string {
	return fmt.Sprintf(uploadedSizeKeyTemplate, absPath)
}

func GetUploadPartsKey(absPath string) string {
	return fmt.Sprintf(partsKeyTemplate, absPath)
}
