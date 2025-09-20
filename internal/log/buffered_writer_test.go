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
	"os"
	"testing"
	"time"
)

// BenchmarkDirectWrite benchmarks direct file write performance.
func BenchmarkDirectWrite(b *testing.B) {
	tmpFile, err := os.CreateTemp(b.TempDir(), "bench_direct_*.log")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	testData := []byte("2025-01-01 12:00:00 [INFO] This is a typical log message with some content\n")

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, err := tmpFile.Write(testData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBufferedWrite benchmarks buffered write performance.
func BenchmarkBufferedWrite(b *testing.B) {
	tmpFile, err := os.CreateTemp(b.TempDir(), "bench_buffered_*.log")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 64KB buffer, 100ms flush interval
	writer := NewBufferedWriter(tmpFile, 64*1024, 100*time.Millisecond)
	defer writer.Close()

	testData := []byte("2025-01-01 12:00:00 [INFO] This is a typical log message with some content\n")

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, err := writer.Write(testData)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Ensure all data is written
	writer.Flush()
}

// BenchmarkConcurrentDirectWrite benchmarks concurrent direct writes.
func BenchmarkConcurrentDirectWrite(b *testing.B) {
	tmpFile, err := os.CreateTemp(b.TempDir(), "bench_concurrent_direct_*.log")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	testData := []byte("2025-01-01 12:00:00 [INFO] This is a typical log message with some content\n")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := tmpFile.Write(testData)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkConcurrentBufferedWrite benchmarks concurrent buffered writes.
func BenchmarkConcurrentBufferedWrite(b *testing.B) {
	tmpFile, err := os.CreateTemp(b.TempDir(), "bench_concurrent_buffered_*.log")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	writer := NewBufferedWriter(tmpFile, 64*1024, 100*time.Millisecond)
	defer writer.Close()

	testData := []byte("2025-01-01 12:00:00 [INFO] This is a typical log message with some content\n")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := writer.Write(testData)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	writer.Flush()
}

// TestBufferedWriter tests basic functionality.
func TestBufferedWriter(t *testing.T) {
	t.Parallel()
	tmpFile, err := os.CreateTemp(t.TempDir(), "test_*.log")
	if err != nil {
		t.Fatal(err)
	}
	fileName := tmpFile.Name()
	defer os.Remove(fileName)

	writer := NewBufferedWriter(tmpFile, 1024, 50*time.Millisecond)

	// Write test data
	testData := []byte("test log message\n")
	for range 100 {
		_, err := writer.Write(testData)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for auto flush
	time.Sleep(150 * time.Millisecond)

	// Close writer
	writer.Close()
	tmpFile.Close()

	// Reopen file to read contents
	content, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}

	expectedSize := len(testData) * 100
	if len(content) != expectedSize {
		t.Errorf("Expected %d bytes, got %d", expectedSize, len(content))
	}
}
