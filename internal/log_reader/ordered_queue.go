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
	"container/heap"
)

// OrderedQueue maintains order of StampedLog elements.
type OrderedQueue struct {
	size       int
	k          int
	queue      []*StampedLog
	sortedLogs *TimestampHeap // Replace slice with priority queue
}

// TimestampHeap implements heap.Interface for StampedLog.
type TimestampHeap []*StampedLog

func (h *TimestampHeap) Len() int { return len(*h) }

func (h *TimestampHeap) Less(i, j int) bool {
	// Sort by timestamp (earlier timestamps come first)
	return (*h)[i].Timestamp.Before(*(*h)[j].Timestamp)
}

func (h *TimestampHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
}

func (h *TimestampHeap) Push(x interface{}) {
	stampedLog, ok := x.(*StampedLog)
	if !ok {
		return
	}
	*h = append(*h, stampedLog)
}

func (h *TimestampHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// NewOrderedQueue creates a new OrderedQueue instance.
func NewOrderedQueue(size int) *OrderedQueue {
	h := &TimestampHeap{}
	heap.Init(h)
	return &OrderedQueue{
		size:       size + 1,
		k:          1,
		queue:      make([]*StampedLog, 0, size+1),
		sortedLogs: h,
	}
}

// Consume adds a new log to the queue and returns the oldest log if queue is full.
func (oq *OrderedQueue) Consume(log *StampedLog) *StampedLog {
	if log.Timestamp == nil {
		// Merge with last log if present
		if len(oq.queue) > 0 {
			lastLog := oq.queue[len(oq.queue)-1]
			lastLog.Line += "\n" + log.Line
		}
		return nil
	}

	// Add to queue and heap
	oq.queue = append(oq.queue, log)
	heap.Push(oq.sortedLogs, log)

	// Check if queue is full
	if len(oq.queue) == oq.size {
		// Pop oldest log from queue
		sl := oq.queue[0]
		oq.queue = oq.queue[1:]

		// Find position in sorted order
		pos := 0
		h := *oq.sortedLogs
		for i := range h {
			if h[i] == sl {
				pos = i
				break
			}
		}

		// Remove from heap
		heap.Remove(oq.sortedLogs, pos)

		// Check if within k-th order
		if pos > oq.k {
			return &StampedLog{Timestamp: nil, Line: sl.Line}
		}

		// Merge all logs with smaller timestamps
		for len(oq.queue) > 0 {
			// Peek at the next log
			nextLog := oq.queue[0]

			if !nextLog.Timestamp.Before(*sl.Timestamp) {
				break
			}

			// Remove from queue and heap
			oq.queue = oq.queue[1:]
			sl.Line += nextLog.Line

			// Find and remove from heap
			for i, curLog := range *oq.sortedLogs {
				if curLog == nextLog {
					heap.Remove(oq.sortedLogs, i)
					break
				}
			}
		}
		return sl
	}

	return nil
}

// DumpRemaining returns all remaining logs in order.
func (oq *OrderedQueue) DumpRemaining() []*StampedLog {
	var remaining []*StampedLog

	for len(oq.queue) > 0 {
		sl := oq.queue[0]
		oq.queue = oq.queue[1:]

		// Find position in sorted order
		pos := 0
		h := *oq.sortedLogs
		for i := range h {
			if h[i] == sl {
				pos = i
				break
			}
		}

		// Remove from heap
		heap.Remove(oq.sortedLogs, pos)

		if pos > oq.k {
			remaining = append(remaining, &StampedLog{Timestamp: nil, Line: sl.Line})
			continue
		}

		// Merge all logs with smaller timestamps
		for len(oq.queue) > 0 {
			nextLog := oq.queue[0]
			if !nextLog.Timestamp.Before(*sl.Timestamp) {
				break
			}
			oq.queue = oq.queue[1:]
			sl.Line += nextLog.Line

			// Find and remove from heap
			for i, curLog := range *oq.sortedLogs {
				if curLog == nextLog {
					heap.Remove(oq.sortedLogs, i)
					break
				}
			}
		}
		remaining = append(remaining, sl)
	}

	return remaining
}
