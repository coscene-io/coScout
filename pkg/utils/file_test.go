package utils

import (
	"github.com/foxglove/mcap/go/mcap"
	log "github.com/sirupsen/logrus"
	"os"
	"testing"
	"time"
)

func TestReadMcap(t *testing.T) {

	filePath := "/Users/yuejinlan/Downloads/rosbag2_2025_03_03-10_51_32_306.mcap"

	mcapFileReader, err := os.Open(filePath)
	if err != nil {
		log.Errorf("open mcap file [%s] failed: %v", filePath, err)
	}
	defer mcapFileReader.Close()

	reader, err := mcap.NewReader(mcapFileReader)
	if err != nil {
		log.Errorf("failed to create mcap reader for mcap file %s: %v", filePath, err)
	}

	info, err := reader.Info()
	if err != nil {
		log.Errorf("failed to get mcap file info: %v", err)
	}

	startNano := info.Statistics.MessageStartTime
	endNano := info.Statistics.MessageEndTime
	//nolint: gosec // ignore uint64 to int64 conversion
	start := time.Unix(int64(startNano/1e9), int64(startNano%1e9))
	//nolint: gosec // ignore uint64 to int64 conversion
	end := time.Unix(int64(endNano/1e9), int64(endNano%1e9))

	log.Infof("start time: %v, end time: %v", start, end)
}
