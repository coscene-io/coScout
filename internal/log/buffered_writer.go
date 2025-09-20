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

package log

import (
	"bufio"
	"io"
	"sync"
	"time"
)

// BufferedWriter wraps any io.Writer with buffering and periodic flushing.
// This is the simplest implementation - no fancy features, just reducing syscalls.
type BufferedWriter struct {
	mu       sync.Mutex
	writer   io.Writer
	buffer   *bufio.Writer
	ticker   *time.Ticker
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewBufferedWriter creates a buffered writer.
// bufferSize: buffer size in bytes, recommended 64KB.
// flushInterval: flush interval, recommended 100ms.
func NewBufferedWriter(w io.Writer, bufferSize int, flushInterval time.Duration) *BufferedWriter {
	if bufferSize <= 0 {
		bufferSize = 64 * 1024 // 64KB default
	}
	if flushInterval <= 0 {
		flushInterval = 100 * time.Millisecond
	}

	bw := &BufferedWriter{
		writer:   w,
		buffer:   bufio.NewWriterSize(w, bufferSize),
		ticker:   time.NewTicker(flushInterval),
		stopChan: make(chan struct{}),
	}

	// Start periodic flush goroutine
	bw.wg.Add(1)
	go bw.flushLoop()

	return bw
}

func (bw *BufferedWriter) Write(p []byte) (n int, err error) {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	n, err = bw.buffer.Write(p)

	// If buffer is nearly full, flush immediately.
	// This avoids blocking due to buffer overflow.
	if bw.buffer.Available() < len(p) {
		_ = bw.buffer.Flush()
	}

	return n, err
}

func (bw *BufferedWriter) flushLoop() {
	defer bw.wg.Done()

	for {
		select {
		case <-bw.ticker.C:
			bw.mu.Lock()
			_ = bw.buffer.Flush()
			bw.mu.Unlock()
		case <-bw.stopChan:
			// Final flush before exit
			bw.mu.Lock()
			_ = bw.buffer.Flush()
			bw.mu.Unlock()
			return
		}
	}
}

// Flush manually flushes the buffer.
func (bw *BufferedWriter) Flush() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.buffer.Flush()
}

// Close closes the buffered writer and ensures all data is flushed.
func (bw *BufferedWriter) Close() error {
	bw.ticker.Stop()
	close(bw.stopChan)
	bw.wg.Wait()

	// If the underlying writer implements io.Closer, close it
	if closer, ok := bw.writer.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}
