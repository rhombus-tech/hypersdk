// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "context"
    "testing"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/stretchr/testify/require"
)

type testSetup struct {
    ctx     context.Context
    manager *Manager
    config  *Config
}

func newTestSetup(t *testing.T) *testSetup {
    cfg := &Config{
        Enabled:       true,
        MaxBlockSize:  1000,
        PingTimeout:   100,
        PongValidators: make([][32]byte, 0),
    }

    return &testSetup{
        ctx:     context.Background(),
        manager: NewManager(cfg, nil),
        config:  cfg,
    }
}

func TestEventEmission(t *testing.T) {
    require := require.New(t)
    ts := newTestSetup(t)

    // Create test event
    contract := codec.CreateAddress(0, ids.GenerateTestID())
    evt := Event{
        Contract:    contract,
        EventType:   "TestEvent",
        Data:       []byte("test data"),
        BlockHeight: 1,
        Timestamp:   uint64(time.Now().Unix()),
    }

    // Test event emission
    ts.manager.Emit(evt)

    // Verify event was stored
    events := ts.manager.GetBlockEvents(1)
    require.Len(events, 1)
    require.Equal(evt, events[0])
}

func TestEventSubscription(t *testing.T) {
    require := require.New(t)
    ts := newTestSetup(t)

    // Create subscription
    eventType := "TestEvent"
sub := ts.manager.Subscribe(EventFilter{
    EventType: &eventType,
})
    defer ts.manager.Unsubscribe(sub.ID)

    // Emit event
    contract := codec.CreateAddress(0, ids.GenerateTestID())
    evt := Event{
        Contract:    contract,
        EventType:   "TestEvent",
        Data:       []byte("test data"),
        BlockHeight: 1,
        Timestamp:   uint64(time.Now().Unix()),
    }
    ts.manager.Emit(evt)

    // Verify subscription received event
    select {
    case received := <-sub.Channel:
        require.Equal(evt, received)
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for event")
    }
}

func TestBlockHandling(t *testing.T) {
    require := require.New(t)
    ts := newTestSetup(t)

    // Commit block
    err := ts.manager.OnBlockCommitted(1, uint64(time.Now().Unix()))
    require.NoError(err)

    // Emit events
    contract := codec.CreateAddress(0, ids.GenerateTestID())
    evt1 := Event{
        Contract:    contract,
        EventType:   "TestEvent",
        BlockHeight: 1,
    }
    evt2 := Event{
        Contract:    contract,
        EventType:   "TestEvent",
        BlockHeight: 1,
    }
    ts.manager.Emit(evt1)
    ts.manager.Emit(evt2)

    // Verify events stored
    events := ts.manager.GetBlockEvents(1)
    require.Len(events, 2)

    // Test rollback
    err = ts.manager.OnBlockRolledBack(0)
    require.NoError(err)

    // Verify events cleared
    events = ts.manager.GetBlockEvents(1)
    require.Len(events, 0)
}