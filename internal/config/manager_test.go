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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/coscene-io/coscout/internal/storage"
	"github.com/stretchr/testify/require"
)

type configTestStorage struct {
	value []byte
}

func (s *configTestStorage) Put(_, _, value []byte) error {
	s.value = append(s.value[:0], value...)
	return nil
}

func (s *configTestStorage) Get(_, _ []byte) ([]byte, error) {
	return append([]byte(nil), s.value...), nil
}

func (s *configTestStorage) Delete(_, _ []byte) error {
	s.value = nil
	return nil
}

func (s *configTestStorage) Close() error {
	return nil
}

func (s *configTestStorage) Iter(_ []byte, _ func(key, value []byte) error) error {
	return nil
}

func TestLoadOnceReturnsInvalidYAMLError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "cos.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("http_server: ["), 0o600))

	manager := InitConfManager(configPath, nil)
	_, err := manager.LoadOnce()

	require.Error(t, err)
}

func TestLoadWithRemoteReturnsLastKnownGoodOnReloadFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "cos.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("http_server:\n  port: 12345\n"), 0o600))

	manager := InitConfManager(configPath, nil)
	first, err := manager.LoadWithRemote()
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 12345, first.HttpServer.Port)

	require.NoError(t, os.WriteFile(configPath, []byte("http_server: ["), 0o600))

	reloaded, err := manager.LoadWithRemote()
	require.Error(t, err)
	require.NotNil(t, reloaded)
	require.Equal(t, *first, *reloaded)
}

func TestLoadWithRemoteLastKnownGoodIsConcurrentSafe(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "cos.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("http_server:\n  port: 12345\n"), 0o600))

	manager := InitConfManager(configPath, nil)
	loaded, err := manager.LoadWithRemote()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	require.NoError(t, os.WriteFile(configPath, []byte("http_server: ["), 0o600))

	managerCopy := *manager
	const readers = 20
	var wg sync.WaitGroup
	results := make(chan error, readers)
	wg.Add(readers)
	for i := range readers {
		go func(manager ConfManager) {
			defer wg.Done()
			reloaded, loadErr := manager.LoadWithRemote()
			if loadErr == nil {
				results <- fmt.Errorf("reload unexpectedly succeeded")
				return
			}
			if reloaded == nil {
				results <- fmt.Errorf("reload returned no last-known-good config")
				return
			}
			if reloaded.HttpServer.Port != 12345 {
				results <- fmt.Errorf("reload port = %d, want 12345", reloaded.HttpServer.Port)
				return
			}
			results <- nil
		}([]ConfManager{*manager, managerCopy}[i%2])
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
}

func TestLoadWithRemoteRejectsInvalidImportedConfig(t *testing.T) {
	configDir := t.TempDir()
	importPath := filepath.Join(configDir, "import.yaml")
	configPath := filepath.Join(configDir, "cos.yaml")
	require.NoError(t, os.WriteFile(importPath, []byte("http_server:\n  port: 23456\n"), 0o600))
	require.NoError(t, os.WriteFile(
		configPath,
		[]byte(fmt.Sprintf("__import__:\n  - file://%s\n", importPath)),
		0o600,
	))

	manager := InitConfManager(configPath, nil)
	loaded, err := manager.LoadWithRemote()
	require.NoError(t, err)
	require.Equal(t, 23456, loaded.HttpServer.Port)

	require.NoError(t, os.WriteFile(importPath, []byte("http_server: ["), 0o600))

	reloaded, err := manager.LoadWithRemote()
	require.Error(t, err)
	require.NotNil(t, reloaded)
	require.Equal(t, 23456, reloaded.HttpServer.Port)
}

func TestLoadWithRemoteRejectsInvalidRemoteConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "cos.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("__import__:\n  - cos://remote\n"), 0o600))

	backend := &configTestStorage{value: []byte(`{"http_server":{"port":34567}}`)}
	var store storage.Storage = backend
	manager := InitConfManager(configPath, &store)

	loaded, err := manager.LoadWithRemote()
	require.NoError(t, err)
	require.Equal(t, 34567, loaded.HttpServer.Port)

	backend.value = []byte("{")

	reloaded, err := manager.LoadWithRemote()
	require.Error(t, err)
	require.NotNil(t, reloaded)
	require.Equal(t, 34567, reloaded.HttpServer.Port)
}
