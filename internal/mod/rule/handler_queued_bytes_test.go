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

package rule

import (
	"context"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/coscene-io/coscout/internal/queuedbytes"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumeRuleMessageReleasesQueuedBytes(t *testing.T) {
	t.Parallel()

	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(80))

	handler := CustomRuleHandler{
		queuedBytes: budget,
	}
	handler.consumeRuleMessage(queuedRuleMessage{
		payload:       []byte(`[{"msg":{"code":1},"topic":"/fault","ts":1}]`),
		reservedBytes: 80,
	})

	assert.Zero(t, budget.Queued())
}

func TestConsumeHTTPRuleBatchPreservesOrder(t *testing.T) {
	t.Parallel()

	var consumed []rule_engine.RuleItem
	err := consumeHTTPRuleBatch(
		[]byte(`[
			{"msg":{"code":1},"topic":"/first","ts":1},
			{"msg":{"code":2},"topic":"/second","ts":2}
		]`),
		func(item rule_engine.RuleItem) {
			consumed = append(consumed, item)
		},
	)

	require.NoError(t, err)
	require.Len(t, consumed, 2)
	assert.Equal(t, "/first", consumed[0].Topic)
	assert.InDelta(t, 1, consumed[0].Ts, 0)
	assert.Equal(t, rule_engine.RuleSourceHTTP, consumed[0].Source)
	assert.Equal(t, "/second", consumed[1].Topic)
	assert.InDelta(t, 2, consumed[1].Ts, 0)
	assert.Equal(t, rule_engine.RuleSourceHTTP, consumed[1].Source)
}

func TestHandleRuleMessageTransfersReservationToQueue(t *testing.T) {
	t.Parallel()

	const size = int64(80)
	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(size))
	require.True(t, budget.TrackMessage("message-1", size))

	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage, 1),
	}
	payload := []byte(`[{"msg":{"code":1},"topic":"/fault","ts":1}]`)
	msg := message.NewMessage("message-1", payload)

	assert.True(t, handler.handleRuleMessage(t.Context(), msg))
	requireMessageAcked(t, msg)
	admitted, transferred := budget.FinishMessage(msg.UUID)
	assert.True(t, admitted)
	assert.True(t, transferred)

	queued := <-handler.ruleMessageChan
	assert.Equal(t, payload, queued.payload)
	assert.Equal(t, size, queued.reservedBytes)
	assert.Equal(t, size, budget.Queued())

	handler.consumeRuleMessage(queued)
	assert.Zero(t, budget.Queued())
}

func TestHandleRuleMessageDelaysDecodeUntilConsumption(t *testing.T) {
	t.Parallel()

	const size = int64(80)
	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(size))
	require.True(t, budget.TrackMessage("message-1", size))

	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage, 1),
	}
	payload := []byte(`[{"msg":[],"topic":"/fault","ts":1}]`)
	msg := message.NewMessage("message-1", payload)

	assert.True(t, handler.handleRuleMessage(t.Context(), msg))
	requireMessageAcked(t, msg)
	admitted, transferred := budget.FinishMessage(msg.UUID)
	assert.True(t, admitted)
	assert.True(t, transferred)

	queued := <-handler.ruleMessageChan
	assert.Equal(t, payload, queued.payload)
	assert.Equal(t, size, budget.Queued())

	handler.consumeRuleMessage(queued)
	assert.Zero(t, budget.Queued())
}

func TestHTTPAndFileMessagesShareFIFOQueue(t *testing.T) {
	t.Parallel()

	const size = int64(80)
	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(size))
	require.True(t, budget.TrackMessage("message-1", size))

	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage, 2),
	}
	fileItem := ruleItemForQueueTest()
	handler.ruleMessageChan <- queuedRuleMessage{item: fileItem}

	payload := []byte(`[{"msg":{"code":1},"topic":"/http","ts":2}]`)
	msg := message.NewMessage("message-1", payload)
	require.True(t, handler.handleRuleMessage(t.Context(), msg))
	requireMessageAcked(t, msg)
	admitted, transferred := budget.FinishMessage(msg.UUID)
	require.True(t, admitted)
	require.True(t, transferred)

	first := <-handler.ruleMessageChan
	second := <-handler.ruleMessageChan
	assert.Equal(t, fileItem, first.item)
	assert.Nil(t, first.payload)
	assert.Equal(t, payload, second.payload)

	handler.consumeRuleMessage(second)
	assert.Zero(t, budget.Queued())
}

func TestEnqueueFileRuleItemStopsWhenQueueIsFullAndCanceled(t *testing.T) {
	t.Parallel()

	handler := CustomRuleHandler{
		ruleMessageChan: make(chan queuedRuleMessage, 1),
	}
	handler.ruleMessageChan <- queuedRuleMessage{item: ruleItemForQueueTest()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.False(t, handler.enqueueFileRuleItem(ctx, ruleItemForQueueTest()))
	assert.Len(t, handler.ruleMessageChan, 1)
}

func TestHandleRuleMessageReleasesReservationWhenCanceled(t *testing.T) {
	t.Parallel()

	const size = int64(80)
	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(size))
	require.True(t, budget.TrackMessage("message-1", size))

	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage),
	}
	msg := message.NewMessage("message-1", []byte(`[{"msg":{"code":1},"topic":"/fault","ts":1}]`))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.False(t, handler.handleRuleMessage(ctx, msg))
	requireMessageAcked(t, msg)
	admitted, transferred := budget.FinishMessage(msg.UUID)
	assert.False(t, admitted)
	assert.True(t, transferred)
	assert.Zero(t, budget.Queued())
}

func TestHandleRuleMessageDropsUnclaimedDelivery(t *testing.T) {
	t.Parallel()

	budget := queuedbytes.NewBudget(100)
	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage, 1),
	}
	msg := message.NewMessage("untracked-message", []byte(`{"msg":{"code":1},"topic":"/fault","ts":1}`))

	assert.True(t, handler.handleRuleMessage(t.Context(), msg))

	requireMessageAcked(t, msg)
	assert.Empty(t, handler.ruleMessageChan)
	assert.Zero(t, budget.Queued())
}

func TestReleaseQueuedRuleMessagesReleasesOnlyHTTPReservations(t *testing.T) {
	t.Parallel()

	budget := queuedbytes.NewBudget(100)
	require.True(t, budget.TryReserve(80))
	handler := CustomRuleHandler{
		queuedBytes:     budget,
		ruleMessageChan: make(chan queuedRuleMessage, 2),
	}
	handler.ruleMessageChan <- queuedRuleMessage{item: ruleItemForQueueTest()}
	handler.ruleMessageChan <- queuedRuleMessage{
		payload:       []byte(`[{"msg":{"code":1},"topic":"/fault","ts":1}]`),
		reservedBytes: 80,
	}

	handler.releaseQueuedRuleMessages()

	assert.Zero(t, budget.Queued())
	assert.Empty(t, handler.ruleMessageChan)
}

func ruleItemForQueueTest() rule_engine.RuleItem {
	return rule_engine.RuleItem{
		Msg:    map[string]interface{}{"code": float64(1)},
		Topic:  "/file",
		Ts:     1,
		Source: "/tmp/test.mcap",
	}
}

func requireMessageAcked(t *testing.T, msg *message.Message) {
	t.Helper()

	select {
	case <-msg.Acked():
	default:
		t.Fatal("message was not acknowledged")
	}
}
