// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/hypersdk/runtime/events"
	"github.com/ava-labs/hypersdk/x/contracts/test"
)

// Basic event emission test
func TestRuntimeEventEmission(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Set current block/height for events
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Call contract that emits event
	result, err := contract.Call("emit_event", "TestEvent", []byte("test data"))
	require.NoError(err)
	require.NotNil(result)

	// Event processing is async, but should complete quickly
	time.Sleep(100 * time.Millisecond)

	// Check events were processed
	events := rt.GetEvents(1)
	require.Len(events, 1)
	require.Equal(contract.Address, events[0].Contract)
	require.Equal("TestEvent", events[0].EventType)
	require.Equal([]byte("test data"), events[0].Data)
}

// Test event subscription
func TestRuntimeEventSubscription(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Create subscription
	eventType := "TestEvent"
	sub := rt.Subscribe(events.EventFilter{
		EventType: &eventType,
	})
	require.NotNil(sub)
	defer rt.Unsubscribe(sub.ID)

	// Set block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Emit event
	_, err = contract.Call("emit_event", eventType, []byte("test data"))
	require.NoError(err)

	// Wait for event processing
	select {
	case evt := <-sub.Channel:
		require.Equal(contract.Address, evt.Contract)
		require.Equal(eventType, evt.EventType)
		require.Equal([]byte("test data"), evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// Test multi-block event processing
func TestRuntimeEventBlockProcessing(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Emit events in multiple blocks
	for height := uint64(1); height <= 3; height++ {
		// Set block
		err = rt.CommitBlock(height, uint64(time.Now().Unix()))
		require.NoError(err)

		// Emit events
		result, err := contract.Call("emit_event", fmt.Sprintf("Event%d", height), []byte{byte(height)})
		require.NoError(err)
		require.NotNil(result)
	}

	// Allow time for processing
	time.Sleep(100 * time.Millisecond)

	// Verify events per block
	for height := uint64(1); height <= 3; height++ {
		events := rt.GetEvents(height)
		require.Len(events, 1)
		require.Equal(fmt.Sprintf("Event%d", height), events[0].EventType)
		require.Equal([]byte{byte(height)}, events[0].Data)
	}
}

// Test block rollback
func TestRuntimeEventRollback(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Setup blocks
	for height := uint64(1); height <= 3; height++ {
		err = rt.CommitBlock(height, uint64(time.Now().Unix()))
		require.NoError(err)

		// Emit event
		_, err := contract.Call("emit_event", "TestEvent", []byte{byte(height)})
		require.NoError(err)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify all events present
	for height := uint64(1); height <= 3; height++ {
		events := rt.GetEvents(height)
		require.Len(events, 1)
	}

	// Rollback to height 1
	err = rt.RollbackBlock(1)
	require.NoError(err)

	// Verify events after height 1 are removed
	for height := uint64(1); height <= 3; height++ {
		events := rt.GetEvents(height)
		if height <= 1 {
			require.Len(events, 1)
		} else {
			require.Len(events, 0)
		}
	}
}

// Test event block limits
func TestRuntimeEventBlockLimits(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	// Create runtime with small block limit
	cfg := NewConfig()
	cfg.EventConfig = &events.Config{
		Enabled:      true,
		MaxBlockSize: 2,
	}
	rt := NewRuntime(cfg, logging.NoLog{})
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Initialize block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Emit events up to limit
	for i := 0; i < 2; i++ {
		_, err = contract.Call("emit_event", "test_event", []byte("event data"))
		require.NoError(err)
	}

	// Try to emit one more event - should fail
	_, err = contract.Call("emit_event", "test_event", []byte("event data"))
	require.Error(err)
	require.ErrorIs(err, events.ErrTooManyEvents)
}

// Test events during state changes
func TestRuntimeEventDuringStateChanges(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Set block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Perform state change that emits event
	result, err := contract.Call("state_change_with_event", []byte("key"), []byte("value"))
	require.NoError(err)
	require.NotNil(result)

	// Verify state change happened immediately
	state := contract.Runtime.StateManager.GetContractState(contract.Address)
	val, err := state.GetValue(ctx, []byte("key"))
	require.NoError(err)
	require.Equal([]byte("value"), val)

	// Allow time for event processing
	time.Sleep(100 * time.Millisecond)

	// Verify event was processed
	events := rt.GetEvents(1)
	require.Len(events, 1)
	require.Equal("StateChanged", events[0].EventType)
}

// Test ping-pong protocol
func TestRuntimePingPong(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Setup validator
	validatorKey := test.GenerateTestKey()
	rt.eventManager.RegisterPongValidator(validatorKey.PublicKey)

	// Set block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Send ping
	_, err = contract.Call("ping")
	require.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Get ping event
	events := rt.GetEvents(1)
	require.Len(events, 1)
	require.Equal(events.EventTypePing, events[0].EventType)

	// Create signed pong response
	pongParams := events.PongParams{
		PingTimestamp: events[0].Timestamp,
		Signature:     test.SignMessage(validatorKey, events[0].Timestamp),
		PublicKey:     validatorKey.PublicKey,
	}

	// Send pong
	_, err = contract.Call("pong", pongParams)
	require.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Verify pong event
	events = rt.GetEvents(1)
	require.Len(events, 2)
	require.Equal(events.EventTypePong, events[1].EventType)
}

// Benchmarks
func BenchmarkEventEmission(b *testing.B) {
	require := require.New(b)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	b.ResetTimer()

	// Measure contract execution speed with event emission
	for i := 0; i < b.N; i++ {
		result, err := contract.Call(
			"emit_event",
			"BenchmarkEvent",
			[]byte{byte(i)},
		)
		require.NoError(err)
		require.NotNil(result)
	}
}

func BenchmarkEventProcessing(b *testing.B) {
	require := require.New(b)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Pre-emit events
	for i := 0; i < b.N; i++ {
		_, err := contract.Call(
			"emit_event",
			"BenchmarkEvent",
			[]byte{byte(i)},
		)
		require.NoError(err)
	}

	b.ResetTimer()

	// Measure event processing time
	startTime := time.Now()
	time.Sleep(100 * time.Millisecond) // Allow processing to complete

	events := rt.GetEvents(1)
	require.Len(events, b.N)

	b.StopTimer()
	
	elapsed := time.Since(startTime)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/sec")
}