// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "testing"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime/events"
    "github.com/stretchr/testify/require"
)

func newTestRuntimeWithEvents(t *testing.T) *WasmRuntime {
    cfg := NewConfig()
    cfg.EventConfig = &events.Config{
        Enabled:      true,
        MaxBlockSize: 1000,
        PingTimeout:  100,
    }

    return NewRuntime(cfg, logging.NoLog{})
}

func TestRuntimeEventIntegration(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()
    
    // Create runtime with events enabled
    runtime := newTestRuntimeWithEvents(t)

    // Deploy test contract
    contract, err := deployEventTestContract(ctx, runtime)
    require.NoError(err)

    // Call contract that emits event
    result, err := runtime.CallContract(ctx, &CallInfo{
        Contract:     contract,
        FunctionName: "emit_test_event",
        Height:      1,
        Timestamp:   uint64(time.Now().Unix()),
    })
    require.NoError(err)
    require.NotNil(result)

    // Verify event was emitted
    events := runtime.Events().GetBlockEvents(1)
    require.Len(events, 1)
    require.Equal("TestEvent", events[0].EventType)
}

func TestPingPongIntegration(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()
    
    runtime := newTestRuntimeWithEvents(t)
    
    // Deploy ping-pong contract
    contract, err := deployPingPongContract(ctx, runtime)
    require.NoError(err)

    // Set up validator
    validator := generateTestValidator()
    runtime.Events().RegisterPongValidator(validator.PublicKey)

    // Call ping
    _, err = runtime.CallContract(ctx, &CallInfo{
        Contract:     contract,
        FunctionName: "ping",
        Height:      1,
        Timestamp:   uint64(time.Now().Unix()),
    })
    require.NoError(err)

    // Verify ping event
    events := runtime.Events().GetBlockEvents(1)
    require.Len(events, 1)
    require.Equal(events.EventTypePing, events[0].EventType)

    // Call pong
    pongParams := events.PongParams{
        PingTimestamp: events[0].Timestamp,
        Signature:    signPong(validator, events[0]),
        PublicKey:    validator.PublicKey,
    }

    _, err = runtime.CallContract(ctx, &CallInfo{
        Contract:     contract,
        FunctionName: "pong",
        Params:      mustSerialize(t, pongParams),
        Height:      2,
        Timestamp:   uint64(time.Now().Unix()),
    })
    require.NoError(err)

    // Verify pong event
    events = runtime.Events().GetBlockEvents(2)
    require.Len(events, 1)
    require.Equal(events.EventTypePong, events[0].EventType)
}

func TestEventBlockLimits(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()
    
    // Create runtime with small block limit
    cfg := NewConfig()
    cfg.EventConfig = &events.Config{
        Enabled:      true,
        MaxBlockSize: 2,
        PingTimeout:  100,
    }
    runtime := NewRuntime(cfg, logging.NoLog{})

    contract, err := deployEventTestContract(ctx, runtime)
    require.NoError(err)

    // Emit events up to limit
    for i := 0; i < 2; i++ {
        _, err = runtime.CallContract(ctx, &CallInfo{
            Contract:     contract,
            FunctionName: "emit_test_event",
            Height:      1,
            Timestamp:   uint64(time.Now().Unix()),
        })
        require.NoError(err)
    }

    // Try to emit one more event
    _, err = runtime.CallContract(ctx, &CallInfo{
        Contract:     contract,
        FunctionName: "emit_test_event",
        Height:      1,
        Timestamp:   uint64(time.Now().Unix()),
    })
    require.Error(err)
    require.ErrorIs(err, events.ErrTooManyEvents)
}

// Helper functions
func deployEventTestContract(ctx context.Context, runtime *WasmRuntime) (codec.Address, error) {
    // Implementation for deploying test contract
    // This would compile and deploy a test contract that emits events
}

func generateTestValidator() *TestValidator {
    // Implementation for generating test validator keys
}

func signPong(validator *TestValidator, pingEvent events.Event) []byte {
    // Implementation for signing pong response
}

func mustSerialize(t *testing.T, v interface{}) []byte {
    data, err := Serialize(v)
    require.NoError(t, err)
    return data
}