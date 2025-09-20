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

package collector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	"github.com/coscene-io/coscout/internal/api"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/name"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/coscene-io/coscout/pkg/upload"
	"github.com/coscene-io/coscout/pkg/utils"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

func Upload(ctx context.Context, reqClient *api.RequestClient, confManager *config.ConfManager, uploadChan chan string, errorChan chan error) {
	log.Infof("Start upload goroutine")
	queue := upload.NewDedupQueue(8)

	go func() {
		// Clean up cache file info periodically
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ticker.Reset(24 * time.Hour) // Reset ticker to 24 hours

				log.Info("Cleaning cache file info")
				if err := cleanCacheFileInfo(confManager.GetStorage()); err != nil {
					log.Errorf("failed to clean cache file info: %v", err)
				}
			case <-ctx.Done():
				log.Info("clean cache file info goroutine done")
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				select {
				case <-ctx.Done():
					log.Info("upload queue goroutine done")
					return
				default:
				}

				//nolint: contextcheck // context is checked in the parent goroutine
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Errorf("upload goroutine panic recovered: %v", r)
							select {
							case errorChan <- errors.Errorf("upload goroutine panic: %v", r):
							default:
								log.Warnf("error channel is full, skip: %v", r)
							}
						}
					}()

					if queue == nil {
						log.Errorf("upload queue is nil, skip")
						return
					}

					if queue.IsEmpty() {
						log.Infof("upload queue is empty, skip")
						return
					}

					log.Infof("upload queue size: %d", queue.Size())
					rcPath, isPop := queue.Pop()
					if !isPop || rcPath == "" {
						log.Infof("upload queue is empty or invalid pop")
						return
					}

					if !utils.CheckReadPath(rcPath) {
						log.Warnf("record cache %s not exist", rcPath)
						return
					}

					data, err := os.ReadFile(rcPath)
					if err != nil {
						log.Errorf("read record cache failed: %v", err)
						return
					}

					rc := model.RecordCache{}
					if err := json.Unmarshal(data, &rc); err != nil {
						log.Errorf("unmarshal record cache failed: %v", err)
						return
					}

					if rc.Uploaded || rc.Skipped {
						log.Infof("Record cache %s has been uploaded or skipped, skip it", rcPath)
						return
					}

					appConfig := confManager.LoadWithRemote()
					if appConfig != nil {
						enabled := appConfig.Upload.NetworkRule.Enabled
						blackInterfaces := appConfig.Upload.NetworkRule.BlackInterfaces
						server := appConfig.Upload.NetworkRule.Server
						if enabled && len(blackInterfaces) > 0 {
							isBlack := utils.CheckNetworkBlackList(server, blackInterfaces)
							if isBlack {
								log.Warnf("Network interface is blacklisted, skipping upload for record %s", rc.GetRecordCachePath())
								return
							}
						}
					}

					manualDisabled := checkDisabledUpload(confManager)
					if manualDisabled {
						log.Warnf("Upload is manual disabled, skipping upload for record %s", rcPath)
						return
					}

					log.Infof("start to upload record %s", rcPath)
					err = uploadFiles(reqClient, confManager, &rc)
					if err != nil {
						select {
						case errorChan <- err:
						default:
							log.Warnf("Error channel is full, dropping error: %v", err)
						}
					}
				}()
			case <-ctx.Done():
				log.Info("upload queue goroutine done")
				return
			}
		}
	}()

	for {
		select {
		case recordCache := <-uploadChan:
			if recordCache == "" {
				log.Warn("received empty record cache path, skip")
				continue
			}

			if queue == nil {
				log.Error("upload queue is nil, skip")
				continue
			}

			if queue.IsFull() {
				log.Warn("upload queue is full, skip")
				continue
			}

			queue.Push(recordCache)
		case <-ctx.Done():
			log.Info("upload goroutine done")
			return
		}
	}
}

func uploadFiles(reqClient *api.RequestClient, confManager *config.ConfManager, recordCache *model.RecordCache) error {
	allCompleted := true
	appConfig := confManager.LoadWithRemote()
	getStorage := confManager.GetStorage()

	recordCache, err := recordCache.Reload()
	if err != nil {
		log.Errorf("failed to reload record cache: %v", err)
		return err
	}

	recordName, ok := recordCache.Record["name"].(string)
	if !ok || len(recordName) == 0 {
		log.Warn("record name is empty")
		return errors.New("record name is empty")
	}

	toUploadFiles := make([]model.FileInfo, 0)
	for filePath, fileInfo := range recordCache.OriginalFiles {
		if lo.Contains(recordCache.UploadedFilePaths, filePath) {
			continue
		}

		if fileInfo.Path == "" {
			fileInfo.Path = filePath
		}
		toUploadFiles = append(toUploadFiles, fileInfo)
	}
	sort.Slice(toUploadFiles, func(i, j int) bool {
		return toUploadFiles[i].Size < toUploadFiles[j].Size
	})

	for _, fileInfo := range toUploadFiles {
		time.Sleep(10 * time.Millisecond) // sleep a while to avoid cpu usage too high
		filePath := fileInfo.Path

		recordCache, err := recordCache.Reload()
		if err != nil {
			log.Errorf("failed to reload record cache: %v", err)
			return err
		}
		if recordCache.Skipped || recordCache.Uploaded {
			log.Infof("record %s has been skipped or uploaded, break!", recordName)
			return nil
		}

		if !utils.CheckReadPath(filePath) {
			log.Warnf("local file %s not exist", filePath)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if lo.Contains(recordCache.UploadedFilePaths, filePath) {
			log.Infof("file %s has been uploaded, skip", filePath)
			continue
		}

		if appConfig != nil {
			enabled := appConfig.Upload.NetworkRule.Enabled
			blackInterfaces := appConfig.Upload.NetworkRule.BlackInterfaces
			server := appConfig.Upload.NetworkRule.Server
			if enabled && len(blackInterfaces) > 0 {
				isBlack := utils.CheckNetworkBlackList(server, blackInterfaces)
				if isBlack {
					return errors.New("network interface is blacklisted, skipping upload")
				}
			}
		}

		disabledUpload := checkDisabledUpload(confManager)
		if disabledUpload {
			return errors.New("upload is manually disabled, skipping upload")
		}

		if err := uploadFile(reqClient, appConfig, getStorage, recordCache.ProjectName, recordName, &fileInfo); err != nil {
			log.Errorf("failed to upload file %s: %v", fileInfo.Path, err)

			allCompleted = false
			continue
		} else {
			log.Infof("upload file %s successfully", fileInfo.Path)

			recordCache, err = recordCache.Reload()
			if err != nil {
				log.Errorf("failed to reload record cache: %v", err)
				return err
			}
			recordCache.UploadedFilePaths = lo.Uniq(append(recordCache.UploadedFilePaths, filePath))
			err = recordCache.Save()
			if err != nil {
				log.Errorf("failed to save record cache: %v", err)
				return err
			}
		}

		if recordCache.UploadTask != nil {
			uploadTaskName, ok := recordCache.UploadTask["name"].(string)
			if ok && len(uploadTaskName) > 0 {
				tags := make(map[string]string)
				tags["uploadedFiles"] = strconv.Itoa(len(lo.Uniq(recordCache.UploadedFilePaths)))

				_, err := reqClient.AddTaskTags(uploadTaskName, tags)
				if err != nil {
					log.Errorf("failed to add task tags: %v", err)
					return err
				}
			}
		}

		if recordCache.DiagnosisTask != nil {
			diagnosisTaskName, ok := recordCache.DiagnosisTask["name"].(string)
			if ok && len(diagnosisTaskName) > 0 {
				tags := make(map[string]string)
				tags["uploadedFiles"] = strconv.Itoa(len(lo.Uniq(recordCache.UploadedFilePaths)))

				_, err := reqClient.AddTaskTags(diagnosisTaskName, tags)
				if err != nil {
					log.Errorf("failed to add task tags: %v", err)
					return err
				}
			}
		}
	}

	//nolint: nestif // no need to nest if
	if allCompleted {
		log.Infof("upload all files successfully")

		var labels []string
		labels = append(labels, recordCache.Labels...)
		labels = append(labels, constant.LabelUploadSuccess)

		_, err := reqClient.UpdateRecordLabels(recordCache.ProjectName, recordName, labels)
		if err != nil {
			log.Errorf("failed to update record labels: %v", err)
			return err
		}

		if recordCache.UploadTask != nil {
			uploadTaskName, ok := recordCache.UploadTask["name"].(string)
			if ok && len(uploadTaskName) > 0 {
				tags := make(map[string]string)
				tags["totalFiles"] = strconv.Itoa(len(recordCache.OriginalFiles))
				tags["recordName"] = recordName

				_, err := reqClient.AddTaskTags(uploadTaskName, tags)
				if err != nil {
					log.Errorf("failed to add task tags: %v", err)
				}

				_, err = reqClient.UpdateTaskState(uploadTaskName, enums.TaskStateEnum_SUCCEEDED.Enum())
				if err != nil {
					log.Errorf("failed to update task state: %v", err)
				}
			}
		}

		if recordCache.DiagnosisTask != nil {
			diagnosisTaskName, ok := recordCache.DiagnosisTask["name"].(string)
			if ok && len(diagnosisTaskName) > 0 {
				_, err = reqClient.UpdateTaskState(diagnosisTaskName, enums.TaskStateEnum_SUCCEEDED.Enum())
				if err != nil {
					log.Errorf("failed to update task state: %v", err)
				}
			}
		}

		recordCache.Uploaded = true
		err = recordCache.Save()
		if err != nil {
			log.Errorf("failed to save record cache: %v", err)
			return err
		}

		log.Infof("record upload finished: %s", recordName)
		rcPath := recordCache.Clean()
		log.Infof("record cache finished, clean up: %s", rcPath)
	}
	return nil
}

func uploadFile(reqClient *api.RequestClient, appConfig *config.AppConfig, storage *storage.Storage, projectName string, recordName string, fileInfo *model.FileInfo) error {
	log.Infof("prepare to upload file %s", fileInfo.Path)
	if fileInfo.Path == "" {
		log.Warn("file path is empty")
		return errors.New("file path is empty")
	}

	fileInfo, cleanUploadCache, err := getFileInfoFromCacheOrFile(storage, fileInfo)
	if err != nil {
		log.Errorf("failed to get file info from cache or file: %v", err)
		return err
	}
	if cleanUploadCache {
		log.Infof("clean upload cache for file %s", fileInfo.Path)
		deleteCacheFileInfo(storage, fileInfo.Path)
	}

	if !appConfig.Collector.SkipCheckSameFile && fileInfo.Sha256 == "" {
		log.Infof("file %s sha256 is empty, calculate it", fileInfo.Path)
		sha256, _, err := utils.CalSha256AndSize(fileInfo.Path, fileInfo.Size)
		if err != nil {
			log.Errorf("failed to calculate sha256: %v", err)
			return err
		}

		log.Infof("file %s sha256 is %s", fileInfo.Path, sha256)
		fileInfo.Sha256 = sha256
	}

	if fileInfo.FileName == "" || fileInfo.FileName == "." {
		fileInfo.FileName = filepath.Base(fileInfo.Path)
	}
	saveCacheFileInfo(storage, fileInfo)

	fileResourceName := name.FileResourceName{
		RecordName: recordName,
		FileName:   fileInfo.FileName,
	}
	if !appConfig.Collector.SkipCheckSameFile {
		if reqClient.CheckCloneFile(recordName, fileResourceName.String(), fileInfo.Sha256) {
			log.Infof("file %s has been cloned, skip", fileInfo.Path)
			return nil
		}
	}

	// create minio client and upload manager first.
	generateSecurityTokenRes, err := reqClient.GenerateSecurityToken(projectName)
	if err != nil {
		log.Errorf("unable to generate security token: %v", err)
		return err
	}
	if generateSecurityTokenRes == nil {
		log.Errorf("generate security token response is nil")
		return errors.New("generate security token response is nil")
	}
	if generateSecurityTokenRes.GetEndpoint() == "" {
		log.Errorf("generate security token endpoint is empty")
		return errors.New("generate security token endpoint is empty")
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			//nolint: gosec// InsecureSkipVerify is used to skip certificate verification
			InsecureSkipVerify: appConfig.Api.Insecure,
		},
		MaxIdleConns:          3,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
	}
	defer transport.CloseIdleConnections()

	mc, err := minio.New(generateSecurityTokenRes.GetEndpoint(), &minio.Options{
		Creds:     credentials.NewStaticV4(generateSecurityTokenRes.GetAccessKeyId(), generateSecurityTokenRes.GetAccessKeySecret(), generateSecurityTokenRes.GetSessionToken()),
		Secure:    true,
		Region:    "",
		Transport: transport,
	})
	if err != nil {
		log.Errorf("unable to create minio client: %v", err)
		return err
	}

	um, err := upload.NewUploadManager(mc, storage, constant.MultiPartUploadBucket, reqClient.GetNetworkChan())
	if err != nil {
		log.Errorf("unable to create upload manager: %v", err)
		return err
	}

	log.Infof("start to upload file %s, size: %d", fileInfo.Path, fileInfo.Size)
	tags := map[string]string{}
	err = um.FPutObject(fileInfo.Path, constant.UploadBucket, fileResourceName.String(), fileInfo.Size, tags, cleanUploadCache)
	if err != nil {
		log.Errorf("failed to upload file %s: %v", fileInfo.Path, err)
		return err
	}

	return nil
}

// just use file size assertion to determine whether file changed, may not be accurate enough.
func getFileInfoFromCacheOrFile(storage *storage.Storage, prevFileInfo *model.FileInfo) (*model.FileInfo, bool, error) {
	cachedFileInfo := getCacheFileInfo(storage, prevFileInfo.Path)
	currentSize, err := utils.GetFileSize(prevFileInfo.Path)
	if err != nil {
		log.Errorf("failed to get file size: %v", err)
		return nil, false, err
	}
	expectedSize := prevFileInfo.Size

	// if expected size is 0 or less, it means we don't know the expected size
	if expectedSize <= 0 {
		if cachedFileInfo.Size > 0 && cachedFileInfo.Size <= currentSize {
			return &model.FileInfo{
				FileName: prevFileInfo.FileName,
				Path:     prevFileInfo.Path,
				Size:     cachedFileInfo.Size,
				Sha256:   cachedFileInfo.Sha256,
			}, false, nil
		}

		return &model.FileInfo{
			FileName: prevFileInfo.FileName,
			Path:     prevFileInfo.Path,
			Size:     currentSize,
			Sha256:   "",
		}, true, nil
	}

	// expected size > 0, need to compare with current size
	if currentSize < expectedSize {
		// current size < expected size, maybe file be written from 0, need to recalculate sha256
		return &model.FileInfo{
			FileName: prevFileInfo.FileName,
			Path:     prevFileInfo.Path,
			Size:     currentSize,
			Sha256:   "",
		}, true, nil
	}

	if expectedSize == cachedFileInfo.Size {
		return &model.FileInfo{
			FileName: prevFileInfo.FileName,
			Path:     prevFileInfo.Path,
			Size:     cachedFileInfo.Size,
			Sha256:   cachedFileInfo.Sha256,
		}, false, nil
	}

	// current size >= expected size && expected size != cached size, file maybe appended, need to recalculate sha256
	return &model.FileInfo{
		FileName: prevFileInfo.FileName,
		Path:     prevFileInfo.Path,
		Size:     expectedSize,
		Sha256:   "",
	}, true, nil
}

func getCacheFileInfo(storage *storage.Storage, file string) *model.FileInfo {
	if storage == nil {
		log.Warn("storage is nil, skip getting cache file info")
		return &model.FileInfo{Size: -1}
	}
	if file == "" {
		log.Warn("file is empty, skip getting cache file info")
		return &model.FileInfo{Size: -1}
	}

	info := model.FileInfo{Size: -1}
	value, err := (*storage).Get([]byte(constant.FileInfoBucket), []byte(file))
	if err != nil {
		log.Warnf("no cached file info: %v", err)
		return &info
	}

	if len(value) == 0 {
		return &info
	}

	if err := json.Unmarshal(value, &info); err != nil {
		log.Errorf("failed to unmarshal file info: %v", err)
		return &info
	}
	fileSize, err := utils.GetFileSize(file)
	if err == nil && fileSize >= 0 {
		if info.Size != fileSize {
			log.Warnf("file size mismatch for %s, cached: %d, actual: %d", file, info.Size, fileSize)
			return &model.FileInfo{Size: -1}
		}
	}

	return &info
}

func saveCacheFileInfo(storage *storage.Storage, info *model.FileInfo) {
	bytes, err := json.Marshal(info)
	if err != nil {
		log.Errorf("failed to marshal file info: %v", err)
		return
	}
	if storage == nil {
		log.Warn("storage is nil, skip saving cache file info")
		return
	}
	if info.Path == "" {
		log.Warn("file path is empty, skip saving cache file info")
		return
	}

	if err := (*storage).Put([]byte(constant.FileInfoBucket), []byte(info.Path), bytes); err != nil {
		log.Errorf("failed to save file info: %v", err)
	}
}

func deleteCacheFileInfo(storage *storage.Storage, file string) {
	if storage == nil {
		log.Warn("storage is nil, skip deleting cache file info")
		return
	}
	if file == "" {
		log.Warn("file is empty, skip deleting cache file info")
		return
	}

	if err := (*storage).Delete([]byte(constant.FileInfoBucket), []byte(file)); err != nil {
		log.Errorf("failed to delete cache file info for %s: %v", file, err)
	}
}

func cleanCacheFileInfo(storage *storage.Storage) error {
	if storage == nil {
		log.Warn("storage is nil, skip cleaning cache file info")
		return nil
	}

	log.Info("cleaning cache file info")
	expiredFiles := make([]string, 0)
	err := (*storage).Iter([]byte(constant.FileInfoBucket), func(key []byte, value []byte) error {
		if utils.CheckReadPath(string(key)) {
			return nil
		}

		expiredFiles = append(expiredFiles, string(key))
		return nil
	})

	if err != nil {
		log.Errorf("failed to iterate cache file info: %v", err)
		return err
	}

	for _, file := range expiredFiles {
		log.Infof("deleting cache file info for %s", file)
		if err := (*storage).Delete([]byte(constant.FileInfoBucket), []byte(file)); err != nil {
			log.Errorf("failed to delete cache file info for %s: %v", file, err)
		}
	}
	return nil
}

func checkDisabledUpload(confManager *config.ConfManager) bool {
	if confManager == nil {
		log.Error("config manager is nil")
		return true
	}

	s := confManager.GetStorage()
	if s == nil {
		log.Error("storage is not initialized")
		return true
	}

	v, err := (*s).Get([]byte(constant.ConfigInfoBucket), []byte(constant.UploadStatusConfigKey))
	if err != nil {
		log.Errorf("failed to get upload status: %v", err)
		return true
	}

	if len(v) == 0 {
		return false
	}

	disabled, err := strconv.ParseBool(string(v))
	if err != nil {
		log.Errorf("failed to parse upload status: %v", err)
		return true
	}

	return disabled
}
