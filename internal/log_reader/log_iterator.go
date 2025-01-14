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
	"io"

	"github.com/sirupsen/logrus"
)

// LogIterator iterates over StampedLogs in order
type LogIterator struct {
	lr        *LogReader
	scanner   *bufio.Scanner
	queue     *OrderedQueue
	toYield   *StampedLog
	remaining []*StampedLog
	offset    int64
}

// NewLogIterator creates a new LogIterator instance
func NewLogIterator(lr *LogReader) (*LogIterator, error) {
	if _, err := lr.reader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	return &LogIterator{
		lr:      lr,
		scanner: bufio.NewScanner(lr.reader),
		queue:   NewOrderedQueue(lr.bufferSize),
	}, nil
}

// Next returns the next StampedLog
func (it *LogIterator) Next() (*StampedLog, bool) {
	// Process each line
	for it.scanner.Scan() {
		line := it.scanner.Text()
		stampedLog := it.lr.parseLogLine(line, it.offset)
		it.offset += stampedLog.Length + 1

		if curLog := it.queue.Consume(stampedLog); curLog != nil {
			if curLog.Timestamp != nil {
				if it.toYield != nil {
					toReturn := it.toYield
					it.toYield = curLog
					return toReturn, true
				}
				it.toYield = curLog
			} else if it.toYield != nil {
				it.toYield.Line += curLog.Line
				it.toYield.Length += curLog.Length + 1
			}
		}
	}

	// Process remaining logs
	if it.remaining == nil {
		it.remaining = it.queue.DumpRemaining()
	}
	for len(it.remaining) > 0 {
		stampedLog := it.remaining[0]
		it.remaining = it.remaining[1:]
		if stampedLog.Timestamp != nil {
			if it.toYield != nil {
				toReturn := it.toYield
				it.toYield = stampedLog
				return toReturn, true
			}
			it.toYield = stampedLog
		} else if it.toYield != nil {
			it.toYield.Line += stampedLog.Line
			it.toYield.Length += stampedLog.Length + 1
		}
	}

	// Send the final log if any
	if it.toYield != nil {
		toReturn := it.toYield
		it.toYield = nil
		return toReturn, true
	}

	if err := it.scanner.Err(); err != nil {
		logrus.Printf("Warning: error reading file: %v", err)
	}

	return nil, false
}
