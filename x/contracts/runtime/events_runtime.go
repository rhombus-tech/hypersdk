// runtime/events_runtime.go
package runtime

import (
    "context"
    
    "github.com/ava-labs/hypersdk/runtime/events"
)

// EventsRuntime provides a runtime wrapper with guaranteed event support
type EventsRuntime struct {
    *WasmRuntime
}

func NewEventsRuntime(cfg *Config, log logging.Logger) (*EventsRuntime, error) {
    if cfg.EventConfig == nil || !cfg.EventConfig.Enabled {
        return nil, errors.New("events must be enabled for EventsRuntime")
    }

    runtime := NewRuntime(cfg, log)
    return &EventsRuntime{runtime}, nil
}

// Event-specific operations
func (r *EventsRuntime) Subscribe(filter events.EventFilter) *events.EventSubscription {
    return r.eventManager.Subscribe(filter)
}

func (r *EventsRuntime) GetBlockEvents(height uint64) []events.Event {
    return r.eventManager.GetBlockEvents(height)
}

func (r *EventsRuntime) RegisterPongValidator(pubKey ed25519.PublicKey) {
    r.eventManager.RegisterPongValidator(pubKey)
}

func (r *EventsRuntime) GetEventStats() events.EventStats {
    return r.eventManager.GetStats()
}