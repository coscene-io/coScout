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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/djherbis/times"
	log "github.com/sirupsen/logrus"
)

func ComputeUploadFiles(taskName string, scanFolders []string, additionalFiles []string, whiteList []string, recursivelyWalkDirs bool, startTime int64, endTime int64) (map[string]model.FileInfo, []string, error) {
	files := make(map[string]model.FileInfo)
	noPermissionFolders := make([]string, 0)
	if err := ValidateTimeWindow(startTime, endTime); err != nil {
		return files, noPermissionFolders, err
	}

	matchesWhitelist := func(filename string) bool {
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

	for _, folder := range scanFolders {
		if !utils.CheckReadPath(folder) {
			log.WithField("taskName", taskName).Warnf("Path %s is not readable, skip!", folder)
			noPermissionFolders = append(noPermissionFolders, folder)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(folder)
		if err != nil {
			log.WithField("taskName", taskName).Errorf("Failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.WithField("taskName", taskName).Warnf("Path %s is not readable, skip!", realPath)

			noPermissionFolders = append(noPermissionFolders, realPath)
			continue
		}

		if !info.IsDir() {
			if !matchesWhitelist(realPath) {
				log.WithField("taskName", taskName).Warnf("Path %s does not match whitelist, skip!", realPath)
				continue
			}
			files[realPath] = model.FileInfo{
				FileName: filepath.Base(realPath),
				Size:     info.Size(),
				Path:     realPath,
			}
			continue
		}

		processFile := func(target map[string]model.FileInfo, root, filePath string, info os.FileInfo) {
			if !matchesWhitelist(filePath) {
				log.WithField("taskName", taskName).Warnf("file path %s does not match whitelist, skip!", filePath)
				return
			}
			timeStat, err := times.Stat(filePath)
			if err != nil {
				log.WithField("taskName", taskName).Errorf("Failed to get file times for %s: %v", filePath, err)
				return
			}

			fileStartTime := info.ModTime()
			fileEndTime := info.ModTime()
			if timeStat.HasBirthTime() {
				log.WithField("taskName", taskName).Infof("file path %s has birth time: %v", filePath, timeStat.BirthTime())
				fileStartTime = timeStat.BirthTime()
			}

			if fileStartTime.Unix() <= endTime && fileEndTime.Unix() >= startTime {
				filename, err := filepath.Rel(root, filePath)
				if err != nil {
					log.WithField("taskName", taskName).Errorf("Failed to get relative path %s: %v", filePath, err)
					filename = filepath.Base(filePath)
				}

				target[filePath] = model.FileInfo{
					FileName: filename,
					Size:     info.Size(),
					Path:     filePath,
				}

				log.WithField("taskName", taskName).Infof("file %s matched time range, file:(%s - %s)", filePath, time.Unix(startTime, 0).UTC().String(), time.Unix(endTime, 0).UTC().String())
			} else {
				log.WithField("taskName", taskName).Warnf("file %s mismatched time range, required: (%s - %s), file: (%s - %s)", filePath, time.Unix(startTime, 0).UTC().String(), time.Unix(endTime, 0).UTC().String(), fileStartTime.UTC().String(), fileEndTime.UTC().String())
			}
		}

		rootDir := realPath
		//nolint: nestif // nested if is acceptable here for clarity.
		if recursivelyWalkDirs {
			folderFiles := make(map[string]model.FileInfo)
			err := utils.WalkFilePaths(rootDir, &utils.SymWalkOptions{
				FollowSymlinks:       true,
				SkipPermissionErrors: true,
				SkipEmptyFiles:       true,
				MaxFiles:             99999,
			}, func(tmpFilePath string) error {
				if !utils.CheckReadPath(tmpFilePath) {
					log.WithField("taskName", taskName).Warnf("Path %s is not readable, skip!", tmpFilePath)
					return nil
				}

				fileAbs, info, err := utils.GetRealFileInfo(tmpFilePath)
				if err != nil {
					log.WithField("taskName", taskName).Errorf("Failed to get file info for %s: %v", tmpFilePath, err)
					return nil
				}

				if !utils.CheckReadPath(fileAbs) {
					log.WithField("taskName", taskName).Warnf("Path %s is not readable, skip!", fileAbs)
					return nil
				}

				if info.IsDir() {
					return nil
				}

				processFile(folderFiles, rootDir, fileAbs, info)
				return nil
			})
			if err != nil {
				log.WithField("taskName", taskName).Errorf("Failed to walk files in folder %s: %v", folder, err)
				continue
			}
			for filePath, fileInfo := range folderFiles {
				files[filePath] = fileInfo
			}
		} else {
			entries, err := os.ReadDir(rootDir)
			if err != nil {
				log.WithField("taskName", taskName).Errorf("Failed to read directory %s: %v", rootDir, err)
				continue
			}

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				joined := filepath.Join(rootDir, entry.Name())
				if !utils.CheckReadPath(joined) {
					log.WithField("taskName", taskName).Warnf("Path %s is not readable, skip!", joined)
					continue
				}

				info, err := entry.Info()
				if err != nil {
					log.WithField("taskName", taskName).Errorf("Failed to get file info for %s: %v", joined, err)
					continue
				}

				processFile(files, realPath, joined, info)
			}
		}
	}

	for _, file := range additionalFiles {
		if !utils.CheckReadPath(file) {
			log.WithField("taskName", taskName).Warnf("AdditionalFiles path %s is not readable, skip!", file)

			noPermissionFolders = append(noPermissionFolders, file)
			continue
		}

		realPath, info, err := utils.GetRealFileInfo(file)
		if err != nil {
			log.WithField("taskName", taskName).Errorf("AdditionalFiles failed to get folder info: %v", err)
			continue
		}

		if !utils.CheckReadPath(realPath) {
			log.WithField("taskName", taskName).Warnf("AdditionalFiles Path %s is not readable, skip!", realPath)

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
		additionalFolderFiles := make(map[string]model.FileInfo)
		err = utils.WalkFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		}, func(tmpAddFilePath string) error {
			if !utils.CheckReadPath(tmpAddFilePath) {
				log.WithField("taskName", taskName).Warnf("AdditionalFiles Path %s is not readable, skip!", tmpAddFilePath)
				return nil
			}

			realPath, info, err := utils.GetRealFileInfo(tmpAddFilePath)
			if err != nil {
				log.WithField("taskName", taskName).Errorf("AdditionalFiles Failed to get file info for %s: %v", tmpAddFilePath, err)
				return nil
			}

			if !utils.CheckReadPath(realPath) {
				log.WithField("taskName", taskName).Warnf("AdditionalFiles Path %s is not readable, skip!", realPath)
				return nil
			}

			filename, err := filepath.Rel(parentFolder, realPath)
			if err != nil {
				log.WithField("taskName", taskName).Errorf("AdditionalFiles Failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			additionalFolderFiles[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
			return nil
		})
		if err != nil {
			log.WithField("taskName", taskName).Errorf("AdditionalFiles Failed to walk through folder %s: %v", realPath, err)
			continue
		}
		for filePath, fileInfo := range additionalFolderFiles {
			files[filePath] = fileInfo
		}
	}

	return files, noPermissionFolders, nil
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
		directoryFiles := make(map[string]model.FileInfo)
		err = utils.WalkFilePaths(realPath, &utils.SymWalkOptions{
			FollowSymlinks:       true,
			SkipPermissionErrors: true,
			SkipEmptyFiles:       true,
			MaxFiles:             99999,
		}, func(filePath string) error {
			realPath, info, err := utils.GetRealFileInfo(filePath)
			if err != nil {
				log.Errorf("failed to stat file %s: %v", realPath, err)
				return nil
			}

			if info.IsDir() {
				return nil
			}

			filename, err := filepath.Rel(baseDir, realPath)
			if err != nil {
				log.Errorf("failed to get relative path: %v", err)
				filename = filepath.Base(realPath)
			}

			directoryFiles[realPath] = model.FileInfo{
				FileName: filename,
				Size:     info.Size(),
				Path:     realPath,
			}
			return nil
		})
		if err != nil {
			log.Errorf("failed to walk file paths: %v", err)
			continue
		}
		for filePath, fileInfo := range directoryFiles {
			files[filePath] = fileInfo
		}
	}

	return files
}

func GetUploadIdKey(absPath string) string {
	return fmt.Sprintf(uploadIdKeyTemplate, buildMultipartCacheScope(absPath, "", ""))
}

func GetUploadedSizeKey(absPath string) string {
	return fmt.Sprintf(uploadedSizeKeyTemplate, buildMultipartCacheScope(absPath, "", ""))
}

func GetUploadPartsKey(absPath string) string {
	return fmt.Sprintf(partsKeyTemplate, buildMultipartCacheScope(absPath, "", ""))
}

func GetScopedUploadIdKey(bucket string, objectKey string, absPath string) string {
	return fmt.Sprintf(uploadIdKeyTemplate, buildMultipartCacheScope(absPath, bucket, objectKey))
}

func GetScopedUploadedSizeKey(bucket string, objectKey string, absPath string) string {
	return fmt.Sprintf(uploadedSizeKeyTemplate, buildMultipartCacheScope(absPath, bucket, objectKey))
}

func GetScopedUploadPartsKey(bucket string, objectKey string, absPath string) string {
	return fmt.Sprintf(partsKeyTemplate, buildMultipartCacheScope(absPath, bucket, objectKey))
}

func buildMultipartCacheScope(absPath string, bucket string, objectKey string) string {
	cleanPath := filepath.Clean(strings.TrimSpace(absPath))
	cleanBucket := strings.TrimSpace(bucket)
	cleanObjectKey := strings.TrimSpace(objectKey)

	if cleanBucket == "" && cleanObjectKey == "" {
		return cleanPath
	}

	sum := sha256.Sum256([]byte(cleanBucket + "\n" + cleanObjectKey + "\n" + cleanPath))
	return "scoped:" + hex.EncodeToString(sum[:])
}
