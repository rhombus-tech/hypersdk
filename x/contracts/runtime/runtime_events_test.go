// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/runtime/events"
	"github.com/stretchr/testify/require"
)

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
	require.True(result.Success)

	// Event processing is async, but should complete quickly
	time.Sleep(100 * time.Millisecond)

	// Check events were processed
	events := rt.GetEvents(1)
	require.Len(events, 1)
	require.Equal(contract.Address, events[0].Contract)
	require.Equal("TestEvent", events[0].EventType)
	require.Equal([]byte("test data"), events[0].Data)

	// Verify event data in result outputs
	eventData, err := Serialize(&events[0])
	require.NoError(err)
	require.Contains(result.Outputs, eventData)
}

func TestRuntimeEventSubscription(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Create subscription
	eventType := "TestEvent"
	sub := rt.eventManager.Subscribe(events.EventFilter{
		EventType: &eventType,
	})
	require.NotNil(sub)
	defer rt.eventManager.Unsubscribe(sub.ID)

	// Set block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Emit event
	result, err := contract.Call("emit_event", eventType, []byte("test data"))
	require.NoError(err)
	require.True(result.Success)

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
		require.True(result.Success)
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
		result, err := contract.Call("emit_event", "TestEvent", []byte{byte(height)})
		require.NoError(err)
		require.True(result.Success)
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

func TestRuntimeEventBlockLimits(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	// Create runtime with small block limit
	cfg := NewConfig()
	cfg.EventConfig = &events.Config{
		Enabled:      true,
		MaxBlockSize: 2,
	}
	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Initialize block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Emit events up to limit
	for i := 0; i < 2; i++ {
		result, err := contract.Call("emit_event", "test_event", []byte("event data"))
		require.NoError(err)
		require.True(result.Success)
	}

	// Try to emit one more event - should fail
	result, err := contract.Call("emit_event", "test_event", []byte("event data"))
	require.NoError(err)
	require.False(result.Success)
	require.Contains(string(result.Error), "too many events")
}

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
	require.True(result.Success)

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

func TestRuntimeContractExecutionWithEvents(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	rt := newTestRuntime(ctx)
	contract, err := rt.newTestContract("event_test")
	require.NoError(err)

	// Set block
	err = rt.CommitBlock(1, uint64(time.Now().Unix()))
	require.NoError(err)

	// Call contract multiple times rapidly
	for i := 0; i < 100; i++ {
		result, err := contract.Call(
			"emit_event",
			fmt.Sprintf("Event%d", i),
			[]byte{byte(i)},
		)
		// Contract execution should complete immediately
		require.NoError(err)
		require.True(result.Success)
	}

	// Allow time for event processing
	time.Sleep(100 * time.Millisecond)

	// Verify all events were processed
	events := rt.GetEvents(1)
	require.Len(events, 100)

	// Verify events are in order
	for i, evt := range events {
		require.Equal(fmt.Sprintf("Event%d", i), evt.EventType)
		require.Equal([]byte{byte(i)}, evt.Data)
	}
}

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
		require.True(result.Success)
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
		result, err := contract.Call(
			"emit_event",
			"BenchmarkEvent",
			[]byte{byte(i)},
		)
		require.NoError(err)
		require.True(result.Success)
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