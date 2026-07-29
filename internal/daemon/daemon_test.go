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

package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/stretchr/testify/require"
)

var errTestMasterServeFailed = errors.New("master serve failed")

func TestRunReturnsMasterBindError(t *testing.T) {
	t.Parallel()

	// #nosec G102 -- this test must reserve every interface used by the daemon.
	listener, err := net.Listen("tcp", ":22525")
	if err == nil {
		t.Cleanup(func() {
			require.NoError(t, listener.Close())
		})
	}

	configPath := filepath.Join(t.TempDir(), "cos.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
master_slave:
  enabled: true
`), 0o600))

	confManager := config.InitConfManager(configPath, nil)
	errorChan := make(chan error, 1)
	runErr := make(chan error, 1)

	go func() {
		runErr <- Run(t.Context(), confManager, nil, errorChan)
	}()

	select {
	case err := <-runErr:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return the master bind error")
	}

	select {
	case err := <-errorChan:
		t.Fatalf("startup error was also sent as a non-fatal runtime error: %v", err)
	default:
	}
}

func TestWaitForMasterReturnsRuntimeError(t *testing.T) {
	t.Parallel()

	masterResult := make(chan error, 1)
	masterResult <- errTestMasterServeFailed
	runCtx, cancel := context.WithCancel(t.Context())

	require.ErrorIs(t, waitForMasterAndCancel(runCtx, masterResult, cancel), errTestMasterServeFailed)
	require.ErrorIs(t, runCtx.Err(), context.Canceled)
}

func TestWaitForMasterCancellationWaitsForShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	masterResult := make(chan error)
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- waitForMasterAndCancel(ctx, masterResult, cancel)
	}()

	cancel()
	select {
	case <-waitResult:
		t.Fatal("waitForMaster returned before master shutdown completed")
	case <-time.After(20 * time.Millisecond):
	}

	masterResult <- nil
	select {
	case err := <-waitResult:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("waitForMaster did not return after master shutdown completed")
	}
}
