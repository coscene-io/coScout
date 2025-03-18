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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/coscene-io/coscout/internal/model"
	"github.com/coscene-io/coscout/internal/storage"
	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"
)

const (
	uploadIdKeyTemplate     = "STORE-KEY-UPLOAD-ID-%s"
	uploadedSizeKeyTemplate = "STORE-KEY-UPLOADED-SIZE-%s"
	partsKeyTemplate        = "STORE-KEY-PARTS-%s"
	maxPartNumber           = 1000
	minPartSize             = 1024 * 1024 * 6               // 6MiB
	maxSinglePutObjectSize  = 1024 * 1024 * 1024 * 1024 * 5 //
)

type FileUploadProgress struct {
	Name      string
	Uploaded  int64
	TotalSize int64
}

// Manager is a manager for uploading files through minio client.
// Note that it's user's responsibility to check the Errs field after Wait() to see if there's any error.
type Manager struct {
	storage            *storage.Storage
	cacheBucket        string
	client             *minio.Client
	uploadProgressChan chan FileUploadProgress
	networkChan        chan *model.NetworkUsage
}

func NewUploadManager(client *minio.Client, storage *storage.Storage, cacheBucket string, networkChan chan *model.NetworkUsage) (*Manager, error) {
	u := &Manager{
		networkChan:        networkChan,
		uploadProgressChan: make(chan FileUploadProgress, 10),
		storage:            storage,
		cacheBucket:        cacheBucket,
		client:             client,
	}

	go u.handleUploadProgress()
	return u, nil
}

// FPutObject uploads a file to a bucket with a key and sha256.
// If the file size is larger than minPartSize, it will use multipart upload.
func (u *Manager) FPutObject(absPath string, bucket string, key string, filesize int64, userTags map[string]string) error {
	var err error
	numThreads := uint(2)

	u.client.TraceOn(log.StandardLogger().WriterLevel(log.DebugLevel))
	ctx := context.Background()
	defer ctx.Done()

	if filesize > int64(minPartSize) {
		partSize := filesize / int64(maxPartNumber)
		if partSize < minPartSize {
			partSize = minPartSize
		}

		u.uploadProgressChan <- FileUploadProgress{Name: absPath, Uploaded: -1, TotalSize: filesize}
		//nolint: gosec // we are not using user input
		err = u.FMultipartPutObject(ctx, bucket, key,
			absPath, filesize, minio.PutObjectOptions{UserTags: userTags, PartSize: uint64(partSize), NumThreads: numThreads})
	} else {
		progress := newUploadProgressReader(absPath, filesize, u.uploadProgressChan)
		_, err = u.client.FPutObject(ctx, bucket, key, absPath,
			minio.PutObjectOptions{Progress: progress, UserTags: userTags, NumThreads: numThreads})
	}

	return err
}

func (u *Manager) handleUploadProgress() {
	fileInfos := make(map[string]int64)
	progressMilestones := []float64{0, 25, 50, 75, 90, 100}

	for progress := range u.uploadProgressChan {
		uploadKey := "upload:" + progress.Name
		totalKey := "total:" + progress.Name

		prevPercent := float64(fileInfos[uploadKey]) / float64(fileInfos[totalKey]) * 100
		if progress.Uploaded > 0 {
			diff := progress.Uploaded - fileInfos[uploadKey]
			if diff > 0 {
				nc := model.NetworkUsage{}
				nc.AddSent(diff)
				u.networkChan <- &nc
			}

			fileInfos[uploadKey] = progress.Uploaded
		}

		if progress.TotalSize > 0 {
			fileInfos[totalKey] = progress.TotalSize
		}

		if fileInfos[totalKey] <= 0 {
			return
		}

		uploadedPercent := float64(fileInfos[uploadKey]) / float64(fileInfos[totalKey]) * 100
		for _, milestone := range progressMilestones {
			if prevPercent < milestone && uploadedPercent >= milestone {
				log.Infof("File: %s, uploaded: %d, total: %d, percent: %.1f", progress.Name, fileInfos[uploadKey], fileInfos[totalKey], uploadedPercent)
				break
			}
		}

		if uploadedPercent >= 100 {
			delete(fileInfos, uploadKey)
			delete(fileInfos, totalKey)

			log.Infof("File: %s uploaded", progress.Name)
			return
		}
	}
}

// FMultipartPutObject uploads a file to a bucket with a key and sha256..
func (u *Manager) FMultipartPutObject(ctx context.Context, bucket string, key string, filePath string, fileSize int64, opts minio.PutObjectOptions) (err error) {
	// Check for largest object size allowed.
	if fileSize > int64(maxSinglePutObjectSize) {
		return errors.Errorf("Your proposed upload size ‘%s’ exceeds the maximum allowed object size ‘%s’ for single PUT operation.", strconv.FormatInt(fileSize, 10), strconv.FormatInt(maxSinglePutObjectSize, 10))
	}

	c := minio.Core{Client: u.client}

	// ----------------- Start fetching previous upload info from db -----------------
	// Fetch upload id. If not found, initiate a new multipart upload.
	var uploadId string
	uploadIdKey := fmt.Sprintf(uploadIdKeyTemplate, filePath)
	uploadIdBytes, err := (*u.storage).Get([]byte(u.cacheBucket), []byte(uploadIdKey))
	if err != nil {
		log.Debugf("Get upload id by: %s warn: %v", uploadIdKey, err)
	}
	if uploadIdBytes != nil {
		uploadId = string(uploadIdBytes)
	}
	if uploadId == "" {
		uploadId, err = c.NewMultipartUpload(ctx, bucket, key, opts)
		if err != nil {
			return errors.Wrap(err, "New multipart upload failed")
		}
	}
	log.Debugf("Get upload id: %s by: %s", uploadId, uploadIdKey)

	// Fetch uploaded size
	var uploadedSize int64
	uploadedSizeKey := fmt.Sprintf(uploadedSizeKeyTemplate, filePath)
	uploadedSizeBytes, err := (*u.storage).Get([]byte(u.cacheBucket), []byte(uploadedSizeKey))
	if err != nil {
		log.Debugf("Get uploaded size by: %s warn: %v", uploadedSizeKey, err)
	}
	if uploadedSizeBytes != nil {
		uploadedSize, err = strconv.ParseInt(string(uploadedSizeBytes), 10, 64)
		if err != nil {
			uploadedSize = 0
		}
	} else {
		uploadedSize = 0
	}

	u.uploadProgressChan <- FileUploadProgress{Name: filePath, Uploaded: uploadedSize, TotalSize: -1}
	log.Debugf("Get uploaded size: %d by: %s", uploadedSize, uploadedSizeKey)

	// Fetch uploaded parts
	var parts []minio.CompletePart
	partsKey := fmt.Sprintf(partsKeyTemplate, filePath)
	partsBytes, err := (*u.storage).Get([]byte(u.cacheBucket), []byte(partsKey))
	if err != nil {
		log.Debugf("Get uploaded parts by: %s warn: %v", partsKey, err)
	}
	if partsBytes != nil {
		err = json.Unmarshal(partsBytes, &parts)
		if err != nil {
			parts = []minio.CompletePart{}
		}
	} else {
		parts = []minio.CompletePart{}
	}
	partNumbers := lo.Map(parts, func(p minio.CompletePart, _ int) int {
		return p.PartNumber
	})
	log.Debugf("Get uploaded parts: %v by: %s", partNumbers, partsKey)
	// ----------------- End fetching previous upload info from db -----------------

	// Set contentType based on filepath extension if not given or default
	// value of "application/octet-stream" if the extension has no associated type.
	if opts.ContentType == "" {
		if opts.ContentType = mime.TypeByExtension(filepath.Ext(filePath)); opts.ContentType == "" {
			opts.ContentType = "application/octet-stream"
		}
	}

	if opts.PartSize == 0 {
		opts.PartSize = minPartSize
	}

	// Calculate the optimal parts info for a given size.
	totalPartsCount, partSize, lastPartSize, err := minio.OptimalPartInfo(fileSize, opts.PartSize)
	if err != nil {
		return errors.Wrap(err, "Optimal part info failed")
	}

	// Declare a channel that sends the next part number to be uploaded.
	uploadPartsCh := make(chan int)
	// Declare a channel that sends back the response of a part upload.
	uploadedPartsCh := make(chan uploadedPartRes)
	// Used for readability, lastPartNumber is always totalPartsCount.
	lastPartNumber := totalPartsCount

	// Send each part number to the channel to be processed.
	go func() {
		defer close(uploadPartsCh)
		for p := 1; p <= totalPartsCount; p++ {
			if slices.Contains(partNumbers, p) {
				log.Debugf("Part: %d already uploaded", p)
				continue
			}
			log.Debugf("Part: %d need to upload", p)
			uploadPartsCh <- p
		}
	}()

	if opts.NumThreads == 0 {
		opts.NumThreads = 4
	}

	// Get reader of the file to be uploaded.
	fileReader, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func(fileReader *os.File) {
		err := fileReader.Close()
		if err != nil {
			log.Errorf("Close file reader failed: %v", err)
		}
	}(fileReader)

	// Starts parallel uploads.
	// Receive the part number to upload from the uploadPartsCh channel.
	//nolint: gosec // we are not using user input
	for w := 1; w <= int(opts.NumThreads); w++ {
		go func() {
			for {
				var partToUpload int
				var ok bool
				select {
				case <-ctx.Done():
					return
				case partToUpload, ok = <-uploadPartsCh:
					if !ok {
						return
					}
				}

				// Calculate the offset and size for the part to be uploaded.
				readOffset := int64(partToUpload-1) * partSize
				curPartSize := partSize
				if partToUpload == lastPartNumber {
					curPartSize = lastPartSize
				}

				sectionReader := io.NewSectionReader(fileReader, readOffset, curPartSize)
				log.Debugf("Uploading part: %d", partToUpload)
				objPart, err := c.PutObjectPart(ctx, bucket, key, uploadId, partToUpload, sectionReader, curPartSize, minio.PutObjectPartOptions{
					SSE: opts.ServerSideEncryption,
				})
				if err != nil {
					log.Debugf("Upload part: %d failed: %v", partToUpload, err)
					uploadedPartsCh <- uploadedPartRes{
						Error: err,
					}
				} else {
					log.Debugf("Upload part: %d success", partToUpload)
					uploadedPartsCh <- uploadedPartRes{
						Part: objPart,
					}
				}
			}
		}()
	}

	// Gather the responses as they occur and update any progress bar
	numToUpload := totalPartsCount - len(partNumbers)
	for m := 1; m <= numToUpload; m++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case uploadRes := <-uploadedPartsCh:
			//nolint: nestif // readability
			if uploadRes.Error != nil {
				if strings.Contains(strings.ToLower(uploadRes.Error.Error()), strings.ToLower("Invalid upload id")) {
					err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(uploadIdKey))
					if err != nil {
						return errors.Wrapf(err, "Delete upload id failed")
					}
					err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(partsKey))
					if err != nil {
						return errors.Wrapf(err, "Delete parts failed")
					}
					err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(uploadedSizeKey))
					if err != nil {
						return errors.Wrapf(err, "Delete uploaded size failed")
					}
				}
				return uploadRes.Error
			}
			// Update the uploadedSize.
			uploadedSize += uploadRes.Part.Size
			parts = append(parts, minio.CompletePart{
				ETag:           uploadRes.Part.ETag,
				PartNumber:     uploadRes.Part.PartNumber,
				ChecksumCRC32:  uploadRes.Part.ChecksumCRC32,
				ChecksumCRC32C: uploadRes.Part.ChecksumCRC32C,
				ChecksumSHA1:   uploadRes.Part.ChecksumSHA1,
				ChecksumSHA256: uploadRes.Part.ChecksumSHA256,
			})

			partsJsonBytes, err := json.Marshal(parts)
			if err != nil {
				log.Errorf("Marshal parts failed: %v", err)
				return err
			}

			err = (*u.storage).Put([]byte(u.cacheBucket), []byte(uploadIdKey), []byte(uploadId))
			if err != nil {
				log.Errorf("Store upload id err: %v", err)
			}
			err = (*u.storage).Put([]byte(u.cacheBucket), []byte(partsKey), partsJsonBytes)
			if err != nil {
				log.Errorf("Store uploaded parts err: %v", err)
			}
			err = (*u.storage).Put([]byte(u.cacheBucket), []byte(uploadedSizeKey), []byte(strconv.FormatInt(uploadedSize, 10)))
			if err != nil {
				log.Errorf("Store uploaded size err: %v", err)
			}

			u.uploadProgressChan <- FileUploadProgress{Name: filePath, Uploaded: uploadedSize, TotalSize: -1}
		}
	}

	// Verify if we uploaded all the data.
	if uploadedSize != fileSize {
		return errors.Wrapf(err, "Uploaded size: %d, file size: %d, does not match", uploadedSize, fileSize)
	}

	// Sort all completed parts.
	slices.SortFunc(parts, func(i, j minio.CompletePart) int {
		return i.PartNumber - j.PartNumber
	})

	_, err = c.CompleteMultipartUpload(ctx, bucket, key, uploadId, parts, opts)
	if err != nil {
		return errors.Wrapf(err, "Complete multipart upload failed")
	}

	err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(uploadIdKey))
	if err != nil {
		return errors.Wrapf(err, "Delete upload id failed")
	}
	err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(partsKey))
	if err != nil {
		return errors.Wrapf(err, "Delete parts failed")
	}
	err = (*u.storage).Delete([]byte(u.cacheBucket), []byte(uploadedSizeKey))
	if err != nil {
		return errors.Wrapf(err, "Delete uploaded size failed")
	}

	return nil
}

type uploadProgressReader struct {
	absPath            string
	total              int64
	uploaded           int64
	uploadProgressChan chan FileUploadProgress
}

func newUploadProgressReader(absPath string, total int64, uploadProgressChan chan FileUploadProgress) *uploadProgressReader {
	uploadProgressChan <- FileUploadProgress{Name: absPath, Uploaded: -1, TotalSize: total}
	return &uploadProgressReader{absPath: absPath, total: total, uploaded: 0, uploadProgressChan: uploadProgressChan}
}

func (r *uploadProgressReader) Read(b []byte) (int, error) {
	n := int64(len(b))
	r.uploaded += n
	r.uploadProgressChan <- FileUploadProgress{Name: r.absPath, Uploaded: r.uploaded, TotalSize: -1}
	return int(n), nil
}

// uploadedPartRes - the response received from a part upload.
type uploadedPartRes struct {
	Error error // Any error encountered while uploading the part.
	Part  minio.ObjectPart
}
