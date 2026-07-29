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

package rule

import (
	"context"
	"testing"
	"time"

	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"github.com/coscene-io/coscout/internal/mod/rule/file_handlers"
	"github.com/coscene-io/coscout/internal/mod/rule/file_state_handler"
	"github.com/coscene-io/coscout/internal/model"
)

func TestHandleCollectInfoTimeWindowLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name          string
		start         time.Time
		end           time.Time
		responses     map[string]*master.TaskResponse
		wantClean     bool
		wantSlaveCall bool
		wantStateScan bool
	}{
		{
			name:      "future local window is consumed as empty success",
			start:     now.Add(10 * time.Minute),
			end:       now.Add(20 * time.Minute),
			wantClean: true,
		},
		{
			name:      "invalid local window is cleaned",
			start:     now,
			end:       now.Add(-time.Minute),
			wantClean: true,
		},
		{
			name:  "invalid slave window wins and is cleaned",
			start: now.Add(-time.Minute),
			end:   now.Add(time.Minute),
			responses: map[string]*master.TaskResponse{
				"future": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
				},
				"invalid": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeInvalidTimeWindow,
				},
			},
			wantClean:     true,
			wantSlaveCall: true,
			wantStateScan: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cleaned := false
			requester := &recordingRuleSlaveFileRequester{responses: tc.responses}
			fileStates := &recordingRuleFileStateHandler{}
			handler := &CustomRuleHandler{
				collectFileStateHandler: fileStates,
				slaveRegistry:           master.NewSlaveRegistry(),
				masterClient:            requester,
				masterConfig:            &config.MasterConfig{RequestTimeout: time.Second},
				cleanCollectInfo: func(model.CollectInfo) string {
					cleaned = true
					return "cleaned"
				},
			}
			info := model.CollectInfo{
				Id: "collect-info",
				Cut: &model.CollectInfoCut{
					Start: tc.start.Unix(),
					End:   tc.end.Unix(),
				},
			}

			handler.handleCollectInfo(info, config.DefaultModConfConfig{})

			if cleaned != tc.wantClean {
				t.Fatalf("cleaned = %v, want %v", cleaned, tc.wantClean)
			}
			if requester.called != tc.wantSlaveCall {
				t.Fatalf("slave called = %v, want %v", requester.called, tc.wantSlaveCall)
			}
			if got := fileStates.updateCalls > 0; got != tc.wantStateScan {
				t.Fatalf("file state scan = %v, want %v", got, tc.wantStateScan)
			}
		})
	}
}

func TestSuccessfulSlaveResponsesPreservesResultsAlongsideLegacyNotReady(t *testing.T) {
	t.Parallel()

	success := &master.TaskResponse{
		Success: true,
		Files: []master.SlaveFileInfo{
			{
				FileInfo: model.FileInfo{Path: "/var/log/healthy.log"},
				SlaveID:  "healthy",
			},
		},
	}
	responses := map[string]*master.TaskResponse{
		"legacy": {
			Success:   false,
			ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
		},
		"healthy": success,
		"missing": nil,
	}

	got := successfulSlaveResponses(responses)
	if len(got) != 1 {
		t.Fatalf("successful response count = %d, want 1", len(got))
	}
	if got["healthy"] != success {
		t.Fatalf("healthy response = %#v, want %#v", got["healthy"], success)
	}
	if _, ok := got["legacy"]; ok {
		t.Fatal("legacy not-ready response was treated as successful")
	}
}

type recordingRuleSlaveFileRequester struct {
	responses map[string]*master.TaskResponse
	called    bool
}

func (r *recordingRuleSlaveFileRequester) RequestAllSlaveFilesByContent(
	context.Context,
	*master.SlaveRegistry,
	*master.TaskRequest,
) map[string]*master.TaskResponse {
	r.called = true
	return r.responses
}

type recordingRuleFileStateHandler struct {
	updateCalls int
}

func (*recordingRuleFileStateHandler) UpdateListenDirs(config.DefaultModConfConfig) error {
	return nil
}

func (r *recordingRuleFileStateHandler) UpdateCollectDirs([]string, config.DefaultModConfConfig) error {
	r.updateCalls++
	return nil
}

func (*recordingRuleFileStateHandler) Files(...file_state_handler.FileFilter) []file_state_handler.FileState {
	return nil
}

func (*recordingRuleFileStateHandler) UpdateFilesProcessState() error {
	return nil
}

func (*recordingRuleFileStateHandler) MarkProcessedFile(string) error {
	return nil
}

func (*recordingRuleFileStateHandler) GetFileHandler(string) file_handlers.Interface {
	return nil
}
