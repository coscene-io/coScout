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

package task

import (
	"context"
	"testing"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/enums"
	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/internal/config"
	"github.com/coscene-io/coscout/internal/master"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHandlePendingTasksKeepsFutureWindowPending(t *testing.T) {
	t.Parallel()

	client := &recordingRequestClient{}
	handler := &CustomTaskHandler{reqClient: client}
	now := time.Now()
	handler.handlePendingTasks([]*resources.Task{
		uploadTaskForWindow("future", now.Add(10*time.Minute), now.Add(20*time.Minute)),
	})

	if len(client.stateUpdates) != 0 {
		t.Fatalf("state updates = %v, want none so the task remains pending", client.stateUpdates)
	}
}

func TestHandlePendingTasksMarksInvalidWindowFailed(t *testing.T) {
	t.Parallel()

	client := &recordingRequestClient{}
	handler := &CustomTaskHandler{reqClient: client}
	now := time.Now()
	handler.handlePendingTasks([]*resources.Task{
		uploadTaskForWindow("invalid", now, now.Add(-time.Minute)),
	})

	if len(client.stateUpdates) != 1 {
		t.Fatalf("state updates = %v, want one permanent failure", client.stateUpdates)
	}
	if got := client.stateUpdates[0]; got != enums.TaskStateEnum_FAILED {
		t.Fatalf("state update = %v, want FAILED", got)
	}
}

func TestHandlePendingTasksKeepsTaskPendingWhenSlaveIsNotReady(t *testing.T) {
	t.Parallel()

	client := &recordingRequestClient{}
	handler := &CustomTaskHandler{
		reqClient:     client,
		slaveRegistry: master.NewSlaveRegistry(),
		masterClient: &recordingSlaveFileRequester{
			responses: map[string]*master.TaskResponse{
				"slow-clock": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
				},
			},
		},
		masterConfig: &config.MasterConfig{RequestTimeout: time.Second},
	}
	now := time.Now()
	handler.handlePendingTasks([]*resources.Task{
		uploadTaskForWindow("slave-future", now.Add(4*time.Minute), now.Add(10*time.Minute)),
	})

	if len(client.stateUpdates) != 0 {
		t.Fatalf("state updates = %v, want none so the task remains pending", client.stateUpdates)
	}
}

func TestHandlePendingTasksMarksFailedWhenAnySlaveWindowIsInvalid(t *testing.T) {
	t.Parallel()

	client := &recordingRequestClient{}
	handler := &CustomTaskHandler{
		reqClient:     client,
		slaveRegistry: master.NewSlaveRegistry(),
		masterClient: &recordingSlaveFileRequester{
			responses: map[string]*master.TaskResponse{
				"slow-clock": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeTimeWindowNotReady,
				},
				"invalid": {
					Success:   false,
					ErrorCode: master.TaskErrorCodeInvalidTimeWindow,
				},
			},
		},
		masterConfig: &config.MasterConfig{RequestTimeout: time.Second},
	}
	now := time.Now()
	handler.handlePendingTasks([]*resources.Task{
		uploadTaskForWindow("slave-invalid", now.Add(-time.Minute), now.Add(time.Minute)),
	})

	if len(client.stateUpdates) != 1 || client.stateUpdates[0] != enums.TaskStateEnum_FAILED {
		t.Fatalf("state updates = %v, want [FAILED]", client.stateUpdates)
	}
}

func TestHandlePendingTasksStillProcessesRunnableWindow(t *testing.T) {
	t.Parallel()

	client := &recordingRequestClient{}
	handler := &CustomTaskHandler{reqClient: client}
	now := time.Now()
	handler.handlePendingTasks([]*resources.Task{
		uploadTaskForWindow("runnable", now.Add(-time.Minute), now),
	})

	if len(client.stateUpdates) != 2 {
		t.Fatalf("state updates = %v, want PROCESSING then SUCCEEDED", client.stateUpdates)
	}
	if client.stateUpdates[0] != enums.TaskStateEnum_PROCESSING ||
		client.stateUpdates[1] != enums.TaskStateEnum_SUCCEEDED {
		t.Fatalf("state updates = %v, want [PROCESSING SUCCEEDED]", client.stateUpdates)
	}
}

type recordingRequestClient struct {
	stateUpdates []enums.TaskStateEnum_TaskState
}

func (c *recordingRequestClient) ListDeviceTasks(string, *enums.TaskStateEnum_TaskState, string) ([]*resources.Task, error) {
	return nil, nil
}

func (c *recordingRequestClient) UpdateTaskState(_ string, state *enums.TaskStateEnum_TaskState) (*resources.Task, error) {
	c.stateUpdates = append(c.stateUpdates, *state)
	return &resources.Task{}, nil
}

func (c *recordingRequestClient) AddTaskTags(string, map[string]string) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

type recordingSlaveFileRequester struct {
	responses map[string]*master.TaskResponse
}

func (r *recordingSlaveFileRequester) RequestAllSlaveFiles(context.Context, *master.SlaveRegistry, *master.TaskRequest) map[string]*master.TaskResponse {
	return r.responses
}

func uploadTaskForWindow(name string, start, end time.Time) *resources.Task {
	return &resources.Task{
		Name: "projects/test/tasks/" + name,
		Detail: &resources.Task_UploadTaskDetail{
			UploadTaskDetail: &resources.UploadTaskDetail{
				StartTime:   timestamppb.New(start),
				EndTime:     timestamppb.New(end),
				ScanFolders: []string{"/must-not-be-accessed"},
			},
		},
	}
}
