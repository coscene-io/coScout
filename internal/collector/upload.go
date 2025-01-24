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
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"

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

func Upload(ctx context.Context, reqClient *api.RequestClient, confManager *config.ConfManager, uploadChan chan *model.RecordCache, errorChan chan error) {
	for {
		select {
		case recordCache := <-uploadChan:
			if recordCache == nil {
				return
			}
			if !utils.CheckReadPath(recordCache.GetRecordCachePath()) {
				log.Warnf("record cache %s not exist", recordCache.GetRecordCachePath())
				return
			}

			//nolint: contextcheck // context is checked in the parent goroutine
			err := uploadFiles(reqClient, confManager, recordCache)
			if err != nil {
				errorChan <- err
			}
		case <-ctx.Done():
			return
		}
	}
}

func uploadFiles(reqClient *api.RequestClient, confManager *config.ConfManager, recordCache *model.RecordCache) error {
	allCompleted := true
	appConfig := confManager.LoadWithRemote()
	getStorage := confManager.GetStorage()

	recordName, ok := recordCache.Record["name"].(string)
	if !ok || len(recordName) == 0 {
		log.Warn("record name is empty")
		return errors.New("record name is empty")
	}

	toUploadFiles := make([]model.FileInfo, 0)
	for filePath, fileInfo := range recordCache.OriginalFiles {
		if fileInfo.Path == "" {
			fileInfo.Path = filePath
		}
		toUploadFiles = append(toUploadFiles, fileInfo)
	}
	sort.Slice(toUploadFiles, func(i, j int) bool {
		return toUploadFiles[i].Size < toUploadFiles[j].Size
	})

	for _, fileInfo := range toUploadFiles {
		filePath := fileInfo.Path

		recordCache, err := recordCache.Reload()
		if err != nil {
			log.Errorf("failed to reload record cache: %v", err)
			return err
		}
		if recordCache.Skipped || recordCache.Uploaded {
			continue
		}

		if !utils.CheckReadPath(filePath) {
			log.Warn(fmt.Sprintf("local file %s not exist", filePath))
			continue
		}

		if lo.Contains(recordCache.UploadedFilePaths, filePath) {
			log.Infof("file %s has been uploaded, skip", filePath)
			continue
		}

		if err := uploadFile(reqClient, appConfig, getStorage, recordCache.ProjectName, recordName, &fileInfo); err != nil {
			log.Errorf("failed to upload file %s: %v", fileInfo.Path, err)

			allCompleted = false
			continue
		} else {
			log.Infof("upload file %s successfully", fileInfo.Path)

			recordCache.UploadedFilePaths = append(recordCache.UploadedFilePaths, filePath)
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
				tags["uploadedFiles"] = strconv.Itoa(len(recordCache.UploadedFilePaths))

				_, err := reqClient.AddTaskTags(uploadTaskName, tags)
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

		_, err := reqClient.UpdateRecordLabels(recordCache.ProjectName, recordName, []string{constant.LabelUploadSuccess})
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

		rcPath := recordCache.Clean()
		log.Infof("record cache finished, clean up: %s", rcPath)
	}
	return nil
}

func uploadFile(reqClient *api.RequestClient, appConfig *config.AppConfig, storage *storage.Storage, projectName string, recordName string, fileInfo *model.FileInfo) error {
	cachedFileInfo := getCacheFileInfo(storage, fileInfo.Path)
	if fileInfo.Size <= 0 {
		if cachedFileInfo.Size > 0 {
			fileInfo.Size = cachedFileInfo.Size
		} else {
			fileSize, err := utils.GetFileSize(fileInfo.Path)
			if err != nil {
				log.Errorf("failed to get file size: %v", err)
				return err
			}
			fileInfo.Size = fileSize
		}
	}

	if !appConfig.Collector.SkipCheckSameFile && fileInfo.Sha256 == "" {
		if cachedFileInfo.Sha256 != "" {
			fileInfo.Sha256 = cachedFileInfo.Sha256
		} else {
			sha256, _, err := utils.CalSha256AndSize(fileInfo.Path, fileInfo.Size)
			if err != nil {
				log.Errorf("failed to calculate sha256: %v", err)
				return err
			}
			fileInfo.Sha256 = sha256
		}
	}

	if fileInfo.FileName == "" {
		fileInfo.FileName = path.Base(fileInfo.Path)
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

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			//nolint: gosec// InsecureSkipVerify is used to skip certificate verification
			InsecureSkipVerify: appConfig.Api.Insecure,
		},
	}
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

	tags := map[string]string{}
	err = um.FPutObject(fileInfo.Path, constant.UploadBucket, fileResourceName.String(), fileInfo.Size, tags)
	if err != nil {
		log.Errorf("failed to upload file %s: %v", fileInfo.Path, err)
		return err
	}

	return nil
}

func getCacheFileInfo(storage *storage.Storage, file string) *model.FileInfo {
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
	return &info
}

func saveCacheFileInfo(storage *storage.Storage, info *model.FileInfo) {
	bytes, err := json.Marshal(info)
	if err != nil {
		log.Errorf("failed to marshal file info: %v", err)
		return
	}

	if err := (*storage).Put([]byte(constant.FileInfoBucket), []byte(info.Path), bytes); err != nil {
		log.Errorf("failed to save file info: %v", err)
	}
}
