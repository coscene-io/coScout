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

package config

import (
	"os"
	"os/user"
	"path"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func GetUserBaseFolder() string {
	baseRelativePath := ".local/state/cos"

	u, err := user.Current()
	if err != nil {
		log.Errorf("Get current user failed: %v", err)
		return path.Join(".", baseRelativePath)
	}
	homeDir := u.HomeDir
	return path.Join(homeDir, baseRelativePath)
}

func GetRecordCacheFolder() string {
	cacheRelativePath := "records"

	cachePath := path.Join(GetUserBaseFolder(), cacheRelativePath)
	dir := filepath.Dir(cachePath)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Panicf("Create cache directory failed: %v", err)
		}
	}
	return cachePath
}

func GetDBPath() string {
	dbRelativePath := "db/cos.db"

	dbPath := path.Join(GetUserBaseFolder(), dbRelativePath)
	dir := filepath.Dir(dbPath)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			log.Panicf("Create db directory failed: %v", err)
		}
	}

	// Check if the file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Create the file if it does not exist
		file, err := os.Create(dbPath)
		if err != nil {
			log.Panicf("Create db file failed: %v", err)
		}
		err = file.Close()
		if err != nil {
			log.Errorf("Close db file failed: %v", err)
		}
	}
	return dbPath
}
