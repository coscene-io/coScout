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

package commands

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthStateStopFromFailedRunDoesNotCancelRestart(t *testing.T) {
	t.Parallel()

	state := &authState{}

	firstCtx, started := state.tryStart()
	require.True(t, started)
	_, started = state.tryStart()
	require.False(t, started)
	require.True(t, state.requestStop())
	require.False(t, state.requestStop())
	require.ErrorIs(t, firstCtx.Err(), context.Canceled)
	_, started = state.tryStart()
	require.False(t, started)

	state.daemonStopped()
	secondCtx, started := state.tryStart()
	require.True(t, started)
	require.NoError(t, secondCtx.Err(), "a previous stop must not cancel a restarted daemon")
}

func TestAuthStateRepeatedAuthorizationTransitionsAreIdempotent(t *testing.T) {
	t.Parallel()

	state := &authState{}
	for range 10 {
		runCtx, started := state.tryStart()
		require.True(t, started)

		_, started = state.tryStart()
		require.False(t, started, "duplicate authorization must not start another daemon")
		require.True(t, state.requestStop())
		require.False(t, state.requestStop(), "duplicate unauthorization must not request another stop")
		require.ErrorIs(t, runCtx.Err(), context.Canceled)

		state.daemonStopped()
	}
}

func TestDaemonStoppedCancelsOldRunBeforeRestart(t *testing.T) {
	t.Parallel()

	state := &authState{}
	firstCtx, started := state.tryStart()
	require.True(t, started)

	state.daemonStopped()

	secondCtx, started := state.tryStart()
	require.True(t, started)
	require.ErrorIs(t, firstCtx.Err(), context.Canceled)
	require.NoError(t, secondCtx.Err())
}

func TestCancelRunAndMarkIdleOrdersCancellationFirst(t *testing.T) {
	t.Parallel()

	var lifecycle atomic.Int32
	lifecycle.Store(daemonRunning)
	runCtx, cancel := context.WithCancel(t.Context())

	cancelRunAndMarkIdle(func() {
		require.Equal(t, daemonRunning, lifecycle.Load(), "idle became visible before the old run was canceled")
		cancel()
	}, &lifecycle)

	require.ErrorIs(t, runCtx.Err(), context.Canceled)
	require.Equal(t, daemonIdle, lifecycle.Load())
}
