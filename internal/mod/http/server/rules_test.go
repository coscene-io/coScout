// Copyright 2025 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/coscene-io/coscout/internal/queuedbytes"
	"github.com/coscene-io/coscout/pkg/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ampleRuleBudget int64 = 1 << 20

func TestRulesHandlerDropsRequestWhenQueuedBytesLimitIsExceeded(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(int64(len(body) - 1))
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("Retry-After"))
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerDropsChunkedRequestWhenQueuedBytesLimitIsExceeded(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(int64(len(body) - 1))
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	request.ContentLength = -1
	handler(recorder, request)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerReleasesQueuedBytesAfterDecodeFailure(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":`)
	budget := queuedbytes.NewBudget(int64(len(body)))
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerRejectsNonObjectMessageWithoutPublishing(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":[{"msg":[{"code":1}],"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(ampleRuleBudget)
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerRejectsMessageNumberThatCannotFitFloat64(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":[{"msg":{"value":1e1000},"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(ampleRuleBudget)
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesPreservesCaseInsensitiveEnvelopeField(t *testing.T) {
	t.Parallel()

	body := `{"Messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`
	budget := queuedbytes.NewBudget(ampleRuleBudget)
	payloads, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.NoError(t, err)
	require.Len(t, payloads, 1)
	assert.InDelta(t, 1, payloads[0].timestamp, 0)
	//nolint:testifylint // Exact bytes verify that the decoder preserves the raw element.
	assert.Equal(t, `{"msg":{"code":1},"topic":"/fault","ts":1}`, string(payloads[0].data))
	releaseQueuedPayloads(budget, payloads)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesUsesLastDuplicateMessagesField(t *testing.T) {
	t.Parallel()

	body := `{"messages":[{"msg":{"code":1},"topic":"/first","ts":1}],"Messages":[{"msg":{"code":2},"topic":"/second","ts":2}]}`
	budget := queuedbytes.NewBudget(ampleRuleBudget)
	payloads, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.NoError(t, err)
	require.Len(t, payloads, 1)
	assert.InDelta(t, 2, payloads[0].timestamp, 0)
	//nolint:testifylint // Exact bytes verify that replacement preserves the raw element.
	assert.Equal(t, `{"msg":{"code":2},"topic":"/second","ts":2}`, string(payloads[0].data))
	assert.Equal(t, int64(len(payloads[0].data))+queuedPayloadOverheadBytes, budget.Queued())

	releaseQueuedPayloads(budget, payloads)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesReleasesPriorPayloadWhenLaterMessageIsInvalid(t *testing.T) {
	t.Parallel()

	body := `{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1},{"msg":[]}]}`
	budget := queuedbytes.NewBudget(ampleRuleBudget)

	_, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.Error(t, err)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesReleasesPriorPayloadWhenLaterMessageExceedsBudget(t *testing.T) {
	t.Parallel()

	first := `{"msg":{"code":1},"topic":"/fault","ts":1}`
	second := `{"msg":{"code":2},"topic":"/fault","ts":2}`
	body := `{"messages":[` + first + `,` + second + `]}`
	budget := queuedbytes.NewBudget(int64(len(first)) + queuedPayloadOverheadBytes)

	_, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.ErrorIs(t, err, queuedbytes.ErrLimitExceeded)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesReleasesPayloadWhenLaterRequestFieldIsInvalid(t *testing.T) {
	t.Parallel()

	body := `{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}],"other":`
	budget := queuedbytes.NewBudget(ampleRuleBudget)

	_, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.Error(t, err)
	assert.Zero(t, budget.Queued())
}

func TestDecodeRuleMessagesAccountsForPerMessageOverhead(t *testing.T) {
	t.Parallel()

	first := `{"msg":null,"ts":1}`
	second := `{"msg":null,"ts":2}`
	body := `{"messages":[` + first + `,` + second + `]}`
	budget := queuedbytes.NewBudget(
		int64(len(first)+len(second)) + 2*queuedPayloadOverheadBytes - 1,
	)

	_, err := decodeRuleMessages(strings.NewReader(body), budget)

	require.ErrorIs(t, err, queuedbytes.ErrLimitExceeded)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerPublishesRawMessagesInTimestampOrder(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	messages, err := pubSub.Subscribe(t.Context(), constant.TopicRuleMsg)
	require.NoError(t, err)

	body := []byte(`{"messages":[{ "msg": { "code": 3 }, "topic": "/fault", "ts": 3 },{"msg":{"code":1},"topic":"/fault","ts":1},{"msg":{"code":2},"topic":"/fault","ts":2}]}`)
	budget := queuedbytes.NewBudget(ampleRuleBudget)
	handler := RulesHandler(pubSub, budget)

	payloads := make(chan string, 3)
	go func() {
		for range 3 {
			msg, ok := <-messages
			if !ok {
				return
			}
			size, claimed := budget.ClaimMessage(msg.UUID)
			if !claimed {
				msg.Nack()
				return
			}
			payloads <- string(msg.Payload)
			budget.Release(size)
			msg.Ack()
		}
	}()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	//nolint:testifylint // Exact bytes verify that the HTTP layer does not re-marshal payloads.
	assert.Equal(t, `{"msg":{"code":1},"topic":"/fault","ts":1}`, <-payloads)
	//nolint:testifylint // Exact bytes verify that the HTTP layer does not re-marshal payloads.
	assert.Equal(t, `{"msg":{"code":2},"topic":"/fault","ts":2}`, <-payloads)
	//nolint:testifylint // Exact bytes verify that the HTTP layer does not re-marshal payloads.
	assert.Equal(t, `{ "msg": { "code": 3 }, "topic": "/fault", "ts": 3 }`, <-payloads)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerReleasesQueuedBytesWhenSubscriberIsUnavailable(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)
	body := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(int64(len(body)) + rawRuleReservationBytes(t, body))
	handler := RulesHandler(pubSub, budget)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerKeepsBytesReservedWhilePublishIsBlocked(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)

	messages, err := pubSub.Subscribe(t.Context(), constant.TopicRuleMsg)
	require.NoError(t, err)

	firstBody := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	secondBody := []byte(`{"messages":[{"msg":{"code":2},"topic":"/fault","ts":2}]}`)
	firstPayloadBytes := rawRuleReservationBytes(t, firstBody)
	budget := queuedbytes.NewBudget(int64(len(firstBody)+len(secondBody)-1) + firstPayloadBytes)
	handler := RulesHandler(pubSub, budget)

	messageReceived := make(chan struct{})
	allowAck := make(chan struct{})
	go func() {
		msg := <-messages
		size, claimed := budget.ClaimMessage(msg.UUID)
		if !claimed {
			msg.Nack()
			return
		}
		close(messageReceived)
		<-allowAck
		budget.Release(size)
		msg.Ack()
	}()

	firstResult := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(firstBody))
		handler(recorder, request)
		firstResult <- recorder.Code
	}()

	select {
	case <-messageReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not reach the subscriber")
	}
	assert.Equal(t, int64(len(firstBody))+firstPayloadBytes, budget.Queued())

	secondRecorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(secondBody))
	handler(secondRecorder, secondRequest)
	assert.Equal(t, http.StatusTooManyRequests, secondRecorder.Code)

	close(allowAck)
	select {
	case status := <-firstResult:
		assert.Equal(t, http.StatusOK, status)
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not complete")
	}
	assert.Zero(t, budget.Queued())
}

func TestRulesHandlerReleasesQueuedBytesAfterSuccessfulRequest(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)

	messages, err := pubSub.Subscribe(t.Context(), constant.TopicRuleMsg)
	require.NoError(t, err)

	body := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	budget := queuedbytes.NewBudget(int64(len(body)) + rawRuleReservationBytes(t, body))
	go func() {
		for msg := range messages {
			size, claimed := budget.ClaimMessage(msg.UUID)
			if !claimed {
				msg.Nack()
				continue
			}
			budget.Release(size)
			msg.Ack()
		}
	}()

	handler := RulesHandler(pubSub, budget)

	for range 2 {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
		handler(recorder, request)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Zero(t, budget.Queued())
	}
}

func TestRulesHandlerTransfersPayloadReservationToSubscriber(t *testing.T) {
	t.Parallel()

	pubSub := newRulesTestPubSub(t)

	messages, err := pubSub.Subscribe(t.Context(), constant.TopicRuleMsg)
	require.NoError(t, err)

	body := []byte(`{"messages":[{"msg":{"code":1},"topic":"/fault","ts":1}]}`)
	payloadBytes := rawRuleReservationBytes(t, body)
	budget := queuedbytes.NewBudget(int64(len(body)) + payloadBytes)
	handler := RulesHandler(pubSub, budget)

	go func() {
		msg := <-messages
		if _, claimed := budget.ClaimMessage(msg.UUID); !claimed {
			msg.Nack()
			return
		}
		msg.Ack()
	}()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/ruleEngine/messages", bytes.NewReader(body))
	handler(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, payloadBytes, budget.Queued())

	budget.Release(payloadBytes)
	assert.Zero(t, budget.Queued())
}

func rawRuleReservationBytes(t *testing.T, body []byte) int64 {
	t.Helper()

	budget := queuedbytes.NewBudget(ampleRuleBudget)
	payloads, err := decodeRuleMessages(bytes.NewReader(body), budget)
	require.NoError(t, err)
	require.Len(t, payloads, 1)
	defer releaseQueuedPayloads(budget, payloads)
	return payloads[0].reservedBytes
}

func newRulesTestPubSub(t *testing.T) *gochannel.GoChannel {
	t.Helper()

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer:            1,
			BlockPublishUntilSubscriberAck: true,
		},
		watermill.NewStdLogger(false, false),
	)
	t.Cleanup(func() {
		require.NoError(t, pubSub.Close())
	})
	return pubSub
}
