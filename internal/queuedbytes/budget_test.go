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
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetRejectsReservationOverLimit(t *testing.T) {
	t.Parallel()

	budget := NewBudget(10)

	_, release, err := budget.ReserveBody(bytes.NewReader([]byte("123456")), 6)
	require.NoError(t, err)
	assert.Equal(t, int64(6), budget.Queued())

	_, _, err = budget.ReserveBody(bytes.NewReader([]byte("12345")), 5)
	require.ErrorIs(t, err, ErrLimitExceeded)
	assert.Equal(t, int64(6), budget.Queued())

	release()
	assert.Zero(t, budget.Queued())
}

func TestBudgetRejectsOverReleaseWithoutChangingOtherReservations(t *testing.T) {
	t.Parallel()

	budget := NewBudget(100)
	require.True(t, budget.TryReserve(60))

	assert.False(t, budget.Release(80))
	assert.Equal(t, int64(60), budget.Queued())
	assert.True(t, budget.Release(60))
	assert.Zero(t, budget.Queued())
}

func TestBudgetTracksUnknownLengthBody(t *testing.T) {
	t.Parallel()

	budget := NewBudget(5)
	reader, release, err := budget.ReserveBody(bytes.NewReader([]byte("123456")), -1)
	require.NoError(t, err)
	defer release()

	_, err = io.ReadAll(reader)
	require.ErrorIs(t, err, ErrLimitExceeded)
	assert.LessOrEqual(t, budget.Queued(), int64(5))
}

func TestBudgetTransfersTrackedMessageToOneSubscriber(t *testing.T) {
	t.Parallel()

	budget := NewBudget(100)
	require.True(t, budget.TryReserve(80))
	require.True(t, budget.TrackMessage("message-1", 80))

	size, claimed := budget.ClaimMessage("message-1")
	require.True(t, claimed)
	assert.Equal(t, int64(80), size)
	_, claimed = budget.ClaimMessage("message-1")
	assert.False(t, claimed)
	assert.True(t, budget.FinishMessage("message-1"))

	budget.Release(size)
	assert.Zero(t, budget.Queued())
}

func TestBudgetKeepsUnclaimedMessageWithPublisher(t *testing.T) {
	t.Parallel()

	budget := NewBudget(100)
	require.True(t, budget.TryReserve(80))
	require.True(t, budget.TrackMessage("message-1", 80))

	assert.False(t, budget.FinishMessage("message-1"))
	budget.Release(80)
	assert.Zero(t, budget.Queued())
}

func TestBudgetMessageOwnershipIsAtomic(t *testing.T) {
	t.Parallel()

	const iterations = 1000
	for i := range iterations {
		budget := NewBudget(1)
		messageID := fmt.Sprintf("message-%d", i)
		require.True(t, budget.TryReserve(1))
		require.True(t, budget.TrackMessage(messageID, 1))

		start := make(chan struct{})
		var claimed, transferred bool
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			_, claimed = budget.ClaimMessage(messageID)
		})
		wg.Go(func() {
			<-start
			transferred = budget.FinishMessage(messageID)
		})

		close(start)
		wg.Wait()
		assert.Equal(t, claimed, transferred)
		budget.Release(1)
		assert.Zero(t, budget.Queued())
	}
}

func TestBudgetConcurrentReservationsNeverExceedLimit(t *testing.T) {
	t.Parallel()

	const (
		limit       = int64(1024)
		reservation = int64(16)
		workers     = 256
	)

	budget := NewBudget(limit)
	start := make(chan struct{})
	releases := make(chan func(), workers)
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			<-start
			_, release, err := budget.ReserveBody(
				bytes.NewReader(make([]byte, reservation)),
				reservation,
			)
			if err == nil {
				releases <- release
				return
			}
			if !errors.Is(err, ErrLimitExceeded) {
				t.Errorf("unexpected reservation error: %v", err)
			}
		})
	}

	close(start)
	wg.Wait()
	close(releases)

	assert.LessOrEqual(t, budget.Queued(), limit)
	for release := range releases {
		release()
	}
	assert.Zero(t, budget.Queued())
}
