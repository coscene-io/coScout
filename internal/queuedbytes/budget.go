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

package queuedbytes

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var ErrLimitExceeded = errors.New("HTTP queued bytes limit exceeded")

// Budget limits the aggregate bytes retained by HTTP rule-message handlers and
// rule messages waiting for the rule engine.
type Budget struct {
	limit        int64
	queued       atomic.Int64
	reservations sync.Map
}

type messageReservation struct {
	size  int64
	state atomic.Uint32
}

const (
	reservationOpen uint32 = iota
	reservationClaimed
	reservationFinished
)

func NewBudget(limit int64) *Budget {
	return &Budget{limit: limit}
}

func (b *Budget) Limit() int64 {
	return b.limit
}

func (b *Budget) Queued() int64 {
	return b.queued.Load()
}

func (b *Budget) TryReserve(size int64) bool {
	if size < 0 || size > b.limit {
		return false
	}

	for {
		queued := b.queued.Load()
		if queued > b.limit-size {
			return false
		}
		if b.queued.CompareAndSwap(queued, queued+size) {
			return true
		}
	}
}

func (b *Budget) Release(size int64) bool {
	if size < 0 {
		return false
	}
	if size == 0 {
		return true
	}

	for {
		queued := b.queued.Load()
		if size > queued {
			return false
		}
		if b.queued.CompareAndSwap(queued, queued-size) {
			return true
		}
	}
}

// TrackMessage records an existing byte reservation before publishing a
// message. The subscriber must claim it before acknowledging the message.
func (b *Budget) TrackMessage(messageID string, size int64) bool {
	if messageID == "" || size <= 0 {
		return false
	}

	_, loaded := b.reservations.LoadOrStore(messageID, &messageReservation{size: size})
	return !loaded
}

// ClaimMessage transfers a tracked reservation to the subscriber. It returns
// false when the message was not produced by the tracked HTTP ingress or when
// another subscriber already claimed it.
func (b *Budget) ClaimMessage(messageID string) (int64, bool) {
	value, ok := b.reservations.Load(messageID)
	if !ok {
		return 0, false
	}

	reservation, ok := value.(*messageReservation)
	if !ok {
		return 0, false
	}
	if !reservation.state.CompareAndSwap(reservationOpen, reservationClaimed) {
		return 0, false
	}
	return reservation.size, true
}

// FinishMessage removes the publisher-side tracking entry after Publish
// returns. A true result means the subscriber owns and must release the bytes.
func (b *Budget) FinishMessage(messageID string) bool {
	value, ok := b.reservations.Load(messageID)
	if !ok {
		return false
	}
	reservation, ok := value.(*messageReservation)
	if !ok {
		return false
	}

	publisherOwns := reservation.state.CompareAndSwap(reservationOpen, reservationFinished)
	b.reservations.Delete(messageID)
	return !publisherOwns && reservation.state.Load() == reservationClaimed
}

// ReserveBody reserves a known Content-Length immediately. For chunked or
// otherwise unknown-length requests, the returned reader reserves bytes as
// they are read. The caller must invoke release after it finishes reading.
func (b *Budget) ReserveBody(body io.Reader, contentLength int64) (io.Reader, func(), error) {
	if contentLength >= 0 {
		if !b.TryReserve(contentLength) {
			return nil, func() {}, ErrLimitExceeded
		}

		var once sync.Once
		return body, func() {
			once.Do(func() {
				b.Release(contentLength)
			})
		}, nil
	}

	reader := &reader{
		reader: body,
		budget: b,
	}
	return reader, reader.release, nil
}

type reader struct {
	reader   io.Reader
	budget   *Budget
	reserved int64
	once     sync.Once
}

func (r *reader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n == 0 {
		return n, err
	}

	if !r.budget.TryReserve(int64(n)) {
		return 0, ErrLimitExceeded
	}
	r.reserved += int64(n)
	return n, err
}

func (r *reader) release() {
	r.once.Do(func() {
		r.budget.Release(r.reserved)
	})
}
