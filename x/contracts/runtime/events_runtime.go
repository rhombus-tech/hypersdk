// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "crypto/ed25519"
    "errors"
    
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/x/contracts/runtime/events"
)

// EventsRuntime provides a runtime wrapper with guaranteed event support
type EventsRuntime struct {
    *WasmRuntime
}

// NewEventsRuntime creates a new runtime with events enabled
func NewEventsRuntime(cfg *Config, log logging.Logger) (*EventsRuntime, error) {
    if cfg.EventConfig == nil || !cfg.EventConfig.Enabled {
        return nil, errors.New("events must be enabled for EventsRuntime")
    }

    runtime := NewRuntime(cfg, log)
    return &EventsRuntime{runtime}, nil
}

// CallContract wraps the WasmRuntime's CallContract to ensure event handling
func (r *EventsRuntime) CallContract(ctx context.Context, callInfo *CallInfo) (*chain.Result, error) {
    result, err := r.WasmRuntime.CallContract(ctx, callInfo)
    if err != nil {
        return nil, err
    }

    // If there are events, add them to the result
    if len(callInfo.events) > 0 {
        for _, evt := range callInfo.events {
            result, err = r.eventManager.EmitWithResult(evt, result)
            if err != nil {
                return &chain.Result{
                    Success: false,
                    Error:   []byte(err.Error()),
                }, nil
            }
        }
    }

    return result, nil
}

// Event-specific operations
func (r *EventsRuntime) Subscribe(filter events.EventFilter) *events.EventSubscription {
    return r.eventManager.Subscribe(filter)
}

func (r *EventsRuntime) Unsubscribe(subscriptionID uint64) {
    r.eventManager.Unsubscribe(subscriptionID)
}

func (r *EventsRuntime) GetBlockEvents(height uint64) []events.Event {
    return r.eventManager.GetBlockEvents(height)
}

func (r *EventsRuntime) GetBlockEventsResult(height uint64) *chain.Result {
    return r.eventManager.GetEventsResult(height)
}

func (r *EventsRuntime) RegisterPongValidator(pubKey ed25519.PublicKey) {
    r.eventManager.RegisterPongValidator(pubKey)
}

func (r *EventsRuntime) GetEventStats() events.EventStats {
    return r.eventManager.GetStats()
}

// Block management
func (r *EventsRuntime) OnBlockCommitted(height uint64, timestamp uint64) error {
    return r.eventManager.OnBlockCommitted(height, timestamp)
}

func (r *EventsRuntime) OnBlockRolledBack(height uint64) error {
    return r.eventManager.OnBlockRolledBack(height)
}

// ValidatePongResponse wraps the event manager's pong validation
func (r *EventsRuntime) ValidatePongResponse(pingTimestamp uint64, signature, publicKey []byte, currentHeight uint64) *chain.Result {
    err := r.eventManager.ValidatePongResponse(pingTimestamp, signature, publicKey, currentHeight)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }
    }
    return &chain.Result{
        Success: true,
    }
}

// Context handling
func (r *EventsRuntime) WithContext(ctx context.Context) context.Context {
    return r.eventManager.WithContext(ctx)
}

// Shutdown handles graceful shutdown of the events runtime
func (r *EventsRuntime) Shutdown(ctx context.Context) error {
    if err := r.eventManager.Shutdown(ctx); err != nil {
        return err
    }
    return r.WasmRuntime.Shutdown(ctx)
}

// Helper method to emit an event with a result
func (r *EventsRuntime) EmitEvent(evt events.Event, result *chain.Result) (*chain.Result, error) {
    return r.eventManager.EmitWithResult(evt, result)
}

// Helper method to create a new event
func (r *EventsRuntime) NewEvent(
    contract codec.Address,
    eventType string,
    data []byte,
    height uint64,
    timestamp uint64,
) (events.Event, error) {
    evt := events.Event{
        Contract:    contract,
        EventType:   eventType,
        Data:       data,
        BlockHeight: height,
        Timestamp:   timestamp,
    }
    if err := r.eventManager.validateEvent(evt); err != nil {
        return events.Event{}, err
    }
    return evt, nil
}

