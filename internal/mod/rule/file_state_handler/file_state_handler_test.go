// Copyright 2026 coScene
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

package file_state_handler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/coscene-io/coscout/internal/config"
	mapset "github.com/deckarep/golang-set/v2"
)

func TestRecursiveDirectoryUpdatesRollBackAfterLaterTraversalError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update func(*fileStateHandler, config.DefaultModConfConfig) error
	}{
		{
			name: "listen dirs",
			update: func(handler *fileStateHandler, conf config.DefaultModConfConfig) error {
				return handler.UpdateListenDirs(conf)
			},
		},
		{
			name: "collect dirs",
			update: func(handler *fileStateHandler, conf config.DefaultModConfConfig) error {
				return handler.UpdateCollectDirs(nil, conf)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			firstPath := filepath.Join(root, "a-file.txt")
			if err := os.WriteFile(firstPath, []byte("data"), 0o600); err != nil {
				t.Fatalf("write first file: %v", err)
			}
			if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "z-broken-link")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			original := FileState{Size: 123, ModifyTime: 456, Unsupported: false}
			handler := &fileStateHandler{
				state:        map[string]FileState{firstPath: original},
				listenDirs:   mapset.NewSet[string](),
				collectDirs:  mapset.NewSet[string](),
				activeTopics: mapset.NewSet[string](),
				statePath:    filepath.Join(t.TempDir(), "state.json"),
			}
			conf := config.DefaultModConfConfig{
				ListenDirs:          []string{root},
				CollectDirs:         []string{root},
				RecursivelyWalkDirs: true,
			}

			if err := tc.update(handler, conf); err != nil {
				t.Fatalf("directory update error = %v", err)
			}
			if !reflect.DeepEqual(handler.state, map[string]FileState{firstPath: original}) {
				t.Fatalf("state = %#v, want original state restored after later traversal error", handler.state)
			}
		})
	}
}
