// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "context"
    "sync"

    "github.com/ava-labs/hypersdk/codec"
)

// Event represents a contract-emitted event
type Event struct {
    Contract    codec.Address
    EventType   string
    Data        []byte
    BlockHeight uint64
    Timestamp   uint64
}

// Manager handles event emission and subscription
type Manager struct {
    mu        sync.RWMutex
    events    []Event
    listeners []chan Event
}

func NewManager() *Manager {
    return &Manager{
        events:    make([]Event, 0),
        listeners: make([]chan Event, 0),
    }
}

func (m *Manager) Subscribe() chan Event {
    m.mu.Lock()
    defer m.mu.Unlock()
    ch := make(chan Event, 100)
    m.listeners = append(m.listeners, ch)
    return ch
}

func (m *Manager) Emit(evt Event) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.events = append(m.events, evt)
    
    for _, listener := range m.listeners {
        select {
        case listener <- evt:
        default:
        }
    }
}

func (m *Manager) GetBlockEvents(height uint64) []Event {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    var blockEvents []Event
    for _, evt := range m.events {
        if evt.BlockHeight == height {
            blockEvents = append(blockEvents, evt)
        }
    }
    return blockEvents
}