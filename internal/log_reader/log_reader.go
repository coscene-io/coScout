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

package log_reader

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"time"
)

const (
	// ChunkSize is used to predict the timestamp schema and timestamps
	ChunkSize = 32 * 1024 // 32KB
)

type TimestampFormat struct {
	regex   *regexp.Regexp
	layout  string
	hasYear bool
	hasDay  bool
}

var (
	// HintOptions defines the timestamp hint formats for filename or first line
	HintOptions = []TimestampFormat{
		{regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}`), "2006-01-02 15:04:05", true, true},
		{regexp.MustCompile(`\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}`), "2006/01/02 15:04:05", true, true},
		{regexp.MustCompile(`\d{10}`), "2006010215", true, true},
	}

	// TSSchema defines the timestamp formats to extract from log lines
	TSSchema = []TimestampFormat{
		{regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d{3}`), "2006-01-02 15:04:05.000", true, true},
		{regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2},\d{3}`), "2006-01-02 15:04:05.000", true, true},
		{regexp.MustCompile(`\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}`), "2006/01/02 15:04:05", true, true},
		{regexp.MustCompile(`\d{4}\s+\d{2}:\d{2}:\d{2}\.\d{6}`), "0102 15:04:05.000000", false, true},
		{regexp.MustCompile(`[a-zA-Z]{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}`), "Jan 2 15:04:05", false, true},
		{regexp.MustCompile(`\d{2}:\d{2}:\d{2}\.\d{3}`), "15:04:05.000", false, false},
	}
)

// StampedLog represents a log line with its timestamp
type StampedLog struct {
	Timestamp *time.Time
	Line      string
	Offset    int64
	Length    int64
}

// LogReader reads and parses log files
type LogReader struct {
	reader     io.ReadSeeker
	bufferSize int
	hint     *time.Time
	tsFormat TimestampFormat
	name     string
}

// NewLogReader creates a new LogReader instance
func NewLogReader(reader io.ReadSeeker, name string) (*LogReader, error) {
	lr := &LogReader{
		reader:     reader,
		name:       name,
		bufferSize: 20,
	}

	// Get timestamp hint from name or first line
	hint, err := lr.getTimestampHint()
	if err != nil {
		return nil, err
	}
	lr.hint = hint

	// Analyze timestamp schema
	tsFormat, err := lr.analyzeTimestampSchema()
	if err != nil {
		return nil, err
	}
	lr.tsFormat = tsFormat

	return lr, nil
}

// GetTimestampFormat returns the timestamp format used in the log file
func (lr *LogReader) GetTimestampFormat() TimestampFormat {
	return lr.tsFormat
}

// GetStartTimestamp returns the first timestamp in the log file
func (lr *LogReader) GetStartTimestamp() (*time.Time, error) {
	if _, err := lr.reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	queue := NewOrderedQueue(lr.bufferSize)
	scanner := bufio.NewScanner(lr.reader)
	bytesRead := 0

	for scanner.Scan() && bytesRead < ChunkSize {
		line := scanner.Text()
		stampedLog := lr.parseLogLine(line, int64(bytesRead))
		bytesRead += len(line) + 1

		if log := queue.Consume(stampedLog); log != nil && log.Timestamp != nil {
			return log.Timestamp, nil
		}
	}

	// Check remaining logs
	for _, log := range queue.DumpRemaining() {
		if log.Timestamp != nil {
			return log.Timestamp, nil
		}
	}

	return nil, fmt.Errorf("no timestamp found in the first %d bytes", ChunkSize)
}

// GetEndTimestamp returns the last timestamp in the log file
func (lr *LogReader) GetEndTimestamp() (*time.Time, error) {
	// Get current size by seeking to end
	size, err := lr.reader.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	startOffset := int64(0)
	if size > ChunkSize {
		startOffset = size - ChunkSize
	}

	_, err = lr.reader.Seek(startOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	queue := NewOrderedQueue(lr.bufferSize)
	scanner := bufio.NewScanner(lr.reader)
	bytesRead := 0

	for scanner.Scan() && bytesRead < ChunkSize {
		line := scanner.Text()
		stampedLog := lr.parseLogLine(line, int64(bytesRead))
		bytesRead += len(line) + 1

		queue.Consume(stampedLog)
	}

	// Get last timestamp from remaining logs
	remainingLogs := queue.DumpRemaining()
	if len(remainingLogs) > 0 {
		return remainingLogs[len(remainingLogs)-1].Timestamp, nil
	}

	return nil, fmt.Errorf("no timestamp found in the last %d bytes", ChunkSize)
}

func (lr *LogReader) getTimestampHint() (*time.Time, error) {
	// Try to get hint from name
	if hint := lr.getHintFromText(lr.name); hint != nil {
		return hint, nil
	}

	// Try to get hint from first line
	if _, err := lr.reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(lr.reader)
	if scanner.Scan() {
		if hint := lr.getHintFromText(scanner.Text()); hint != nil {
			return hint, nil
		}
	}

	return nil, nil
}

func (lr *LogReader) getHintFromText(text string) *time.Time {
	for _, opt := range HintOptions {
		if match := opt.regex.FindString(text); match != "" {
			if t, err := time.ParseInLocation(opt.layout, match, time.FixedZone("UTC+8", 8*60*60)); err == nil {
				return &t
			}
		}
	}
	return nil
}

func (lr *LogReader) analyzeTimestampSchema() (TimestampFormat, error) {
	if _, err := lr.reader.Seek(0, io.SeekStart); err != nil {
		return TimestampFormat{}, err
	}

	// Create queues for each schema
	queues := make([]*OrderedQueue, len(TSSchema))
	counts := make([]int, len(TSSchema))
	for i := range TSSchema {
		queues[i] = NewOrderedQueue(lr.bufferSize)
	}

	scanner := bufio.NewScanner(lr.reader)
	bytesRead := 0

	for scanner.Scan() && bytesRead < ChunkSize {
		line := scanner.Text()
		for i, schema := range TSSchema {
			if match := schema.regex.FindString(line); match != "" {
				if t, err := time.ParseInLocation(schema.layout, match, time.FixedZone("UTC+8", 8*60*60)); err == nil {
					stampedLog := &StampedLog{
						Timestamp: lr.adjustTimestamp(&t),
						Line:      line,
						Offset:    int64(bytesRead),
						Length:    int64(len(line)),
					}
					if log := queues[i].Consume(stampedLog); log != nil && log.Timestamp != nil {
						counts[i]++
					}
				}
			}
		}
		bytesRead += len(line) + 1
	}

	// Find schema with the highest count
	maxCount := 0
	maxIdx := 0
	for i, count := range counts {
		total := count + len(queues[i].DumpRemaining())
		if total > maxCount {
			maxCount = total
			maxIdx = i
		}
	}

	return TSSchema[maxIdx], nil
}

func (lr *LogReader) parseLogLine(line string, offset int64) *StampedLog {
	if match := lr.tsFormat.regex.FindString(line); match != "" {
		if t, err := time.ParseInLocation(lr.tsFormat.layout, match, time.FixedZone("UTC+8", 8*60*60)); err == nil {
			return &StampedLog{
				Timestamp: lr.adjustTimestamp(&t),
				Line:      line,
				Offset:    offset,
				Length:    int64(len(line)),
			}
		}
	}
	return &StampedLog{
		Line:   line,
		Offset: offset,
		Length: int64(len(line)),
	}
}

func (lr *LogReader) adjustTimestamp(t *time.Time) *time.Time {
	if lr.hint == nil {
		now := time.Now()
		if !lr.tsFormat.hasDay {
			// Missing full date
			candidate := time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.FixedZone("UTC+8", 8*60*60))
			if candidate.After(now) {
				candidate = candidate.AddDate(0, 0, -1)
			}
			return &candidate
		} else if !lr.tsFormat.hasYear {
			// Missing year
			candidate := time.Date(now.Year(), t.Month(), t.Day(),
				t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.FixedZone("UTC+8", 8*60*60))
			if candidate.After(now) {
				candidate = candidate.AddDate(-1, 0, 0)
			}
			return &candidate
		}
		return t
	}

	if !lr.tsFormat.hasDay {
		candidate := time.Date(lr.hint.Year(), lr.hint.Month(), lr.hint.Day(),
			t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.FixedZone("UTC+8", 8*60*60))
		if candidate.Before(*lr.hint) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return &candidate
	} else if !lr.tsFormat.hasYear {
		candidate := time.Date(lr.hint.Year(), t.Month(), t.Day(),
			t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.FixedZone("UTC+8", 8*60*60))
		if candidate.Before(*lr.hint) {
			candidate = candidate.AddDate(1, 0, 0)
		}
		return &candidate
	}
	return t
}
