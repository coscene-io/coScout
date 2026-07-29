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

package file_handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/require"
)

func TestLogHandlerCancellationUnblocksFullRuleItemChannel(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "application.log")
	require.NoError(t, os.WriteFile(
		logPath,
		[]byte("2025-01-01 00:00:00.000 INFO started\n"),
		0o600,
	))

	ruleItems := make(chan rule_engine.RuleItem)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(done)
		result <- NewLogHandler().SendRuleItems(
			ctx,
			logPath,
			mapset.NewSet[string](),
			ruleItems,
		)
	}()

	select {
	case <-done:
		t.Fatal("log handler returned before its blocked channel send was cancelled")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("log handler did not return after cancellation")
	}
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestHandlersReturnErrorsForUnreadableFiles(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing")
	ruleItems := make(chan rule_engine.RuleItem, 1)
	activeTopics := mapset.NewSet[string]()

	for name, handler := range map[string]Interface{
		"log":  NewLogHandler(),
		"mcap": NewMcapHandler(),
		"ros1": NewRos1Handler(),
	} {
		t.Run(name, func(t *testing.T) {
			err := handler.SendRuleItems(
				context.Background(),
				missingPath,
				activeTopics,
				ruleItems,
			)
			require.Error(t, err)
		})
	}
}

func TestIntentionalSkipsReturnSuccess(t *testing.T) {
	ruleItems := make(chan rule_engine.RuleItem, 1)

	require.NoError(t, NewDefaultHandler().SendRuleItems(
		context.Background(),
		"unsupported.file",
		mapset.NewSet[string](),
		ruleItems,
	))
	require.NoError(t, NewLogHandler().SendRuleItems(
		context.Background(),
		"missing.log",
		mapset.NewSet[string]("/some_other_topic"),
		ruleItems,
	))
}

func TestLogHandlerEOFReturnsSuccess(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "application.log")
	require.NoError(t, os.WriteFile(
		logPath,
		[]byte("2025-01-01 00:00:00.000 INFO complete\n"),
		0o600,
	))
	ruleItems := make(chan rule_engine.RuleItem, 1)

	require.NoError(t, NewLogHandler().SendRuleItems(
		context.Background(),
		logPath,
		mapset.NewSet[string](),
		ruleItems,
	))
	require.Len(t, ruleItems, 1)
}
