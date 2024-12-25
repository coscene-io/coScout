package collector

import (
	"encoding/json"
	"errors"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/model"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func SaveRecordCache(rc *model.RecordCache) error {
	baseFolder := config.GetRecordCacheFolder()

	seconds := rc.Timestamp / 1000
	milliseconds := rc.Timestamp % 1000
	dirName := time.Unix(seconds, 0).UTC().Format("2006-01-02-15-04-05") + "_" + strconv.Itoa(int(milliseconds))

	dirPath := filepath.Join(baseFolder, dirName, ".cos")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			log.Errorf("create cache directory failed: %v", err)
			return errors.New("create cache directory failed")
		}
	}

	file := filepath.Join(dirPath, "state.json")
	data, err := json.Marshal(rc)
	if err != nil {
		log.Errorf("marshal record cache failed: %v", err)
		return errors.New("marshal record cache failed")
	}
	err = os.WriteFile(file, data, 0644)
	if err != nil {
		log.Errorf("write record cache failed: %v", err)
		return errors.New("write record cache failed")
	}
	return nil
}

func FindAllRecordCaches() []string {
	baseFolder := config.GetRecordCacheFolder()
	var records []string

	err := filepath.Walk(baseFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Errorf("walk through cache directory failed: %v", err)
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".cos/state.json") {
			records = append(records, path)
		}
		return nil
	})
	if err != nil {
		log.Errorf("walk through cache directory failed: %v", err)
	}
	return records
}
