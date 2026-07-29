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

package master

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/stretchr/testify/require"
)

func TestServerStartReturnsBindErrorWithoutReportingReady(t *testing.T) {
	t.Parallel()

	// #nosec G102 -- this test must reserve every interface used by the server.
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port
	server := NewServer(port, config.DefaultMasterConfig())

	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(t.Context())
	}()

	select {
	case err := <-startErr:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("Start did not return the bind error")
	}

	select {
	case <-server.Ready():
		t.Fatal("server reported ready despite the bind error")
	default:
	}
}

func TestServerStartReportsReadyAfterListenAndStopsOnCancellation(t *testing.T) {
	t.Parallel()

	server := NewServer(0, config.DefaultMasterConfig())
	ctx, cancel := context.WithCancel(t.Context())
	startErr := make(chan error, 1)

	go func() {
		startErr <- server.Start(ctx)
	}()

	select {
	case <-server.Ready():
	case err := <-startErr:
		t.Fatalf("Start returned before reporting ready: %v", err)
	case <-time.After(time.Second):
		t.Fatal("server did not report ready after listening")
	}

	cancel()

	select {
	case err := <-startErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}
