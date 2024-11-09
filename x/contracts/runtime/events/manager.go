// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "context"
    "crypto/ed25519"
    "sync"
    "time"

    "github.com/ava-labs/hypersdk/codec"
)

// Manager handles event emission, subscription, and validation
type Manager struct {
    // Protected by mu
    mu              sync.RWMutex
    events          []Event
    blockEvents     map[uint64][]Event
    listeners       []chan Event
    subscriptions   map[uint64]*EventSubscription
    nextSubID       uint64
    
    // Block management
    currentBlock    uint64
    currentTime     uint64
    eventsInBlock   uint64
    
    // Configuration
    config          *Config
    options         *EventOptions
    
    // Stats
    stats           *EventStats
    
    // Ping-pong state
    activePings     map[uint64]*PingState
    pongValidators  map[string]ed25519.PublicKey
}

type PingState struct {
    Timestamp uint64
    Sender    codec.Address
    ExpiresAt uint64
}

// NewManager creates a new event manager
func NewManager(cfg *Config, opts *EventOptions) *Manager {
    if opts == nil {
        opts = DefaultEventOptions()
    }
    
    return &Manager{
        blockEvents:    make(map[uint64][]Event),
        subscriptions:  make(map[uint64]*EventSubscription),
        activePings:    make(map[uint64]*PingState),
        pongValidators: make(map[string]ed25519.PublicKey),
        config:         cfg,
        options:        opts,
        stats:         &EventStats{},
    }
}

// Emit adds an event to the manager
func (m *Manager) Emit(evt Event) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if !m.config.Enabled {
        return
    }

    // Add to main events list
    m.events = append(m.events, evt)
    
    // Add to block events
    m.blockEvents[evt.BlockHeight] = append(m.blockEvents[evt.BlockHeight], evt)
    m.eventsInBlock++
    
    // Update stats
    m.stats.TotalProcessed++
    m.stats.LastProcessed = uint64(time.Now().Unix())

    // Notify subscribers
    for _, listener := range m.listeners {
        select {
        case listener <- evt:
        default:
            // Skip if channel is full
        }
    }

    // Process subscriptions
    for _, sub := range m.subscriptions {
        if sub.Filter.Matches(&evt) {
            select {
            case sub.Channel <- evt:
            default:
                // Skip if channel is full
            }
        }
    }
}

// Subscribe creates a new event subscription
func (m *Manager) Subscribe(filter EventFilter) *EventSubscription {
    m.mu.Lock()
    defer m.mu.Unlock()

    id := m.nextSubID
    m.nextSubID++

    sub := &EventSubscription{
        ID:      id,
        Filter:  filter,
        Channel: make(chan Event, m.options.BufferSize),
    }

    m.subscriptions[id] = sub
    return sub
}

// Unsubscribe removes a subscription
func (m *Manager) Unsubscribe(id uint64) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if sub, exists := m.subscriptions[id]; exists {
        close(sub.Channel)
        delete(m.subscriptions, id)
    }
}

// Block management functions
func (m *Manager) OnBlockCommitted(height uint64, timestamp uint64) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.currentBlock = height
    m.currentTime = timestamp
    m.eventsInBlock = 0

    // Clean up old ping states
    m.cleanupExpiredPings(height)

    return nil
}

func (m *Manager) OnBlockRolledBack(height uint64) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if height >= m.currentBlock {
        return NewBlockError(height, ErrFutureBlock)
    }

    // Remove events from rolled back blocks
    for h := range m.blockEvents {
        if h > height {
            delete(m.blockEvents, h)
        }
    }

    m.currentBlock = height
    return nil
}

// GetBlockEvents returns events for a specific block
func (m *Manager) GetBlockEvents(height uint64) []Event {
    m.mu.RLock()
    defer m.mu.RUnlock()

    return m.blockEvents[height]
}

// CheckBlockLimit verifies block hasn't exceeded max events
func (m *Manager) CheckBlockLimit(height uint64) error {
    m.mu.RLock()
    defer m.mu.RUnlock()

    if m.eventsInBlock >= m.config.MaxBlockSize {
        return ErrTooManyEvents
    }
    return nil
}

// Ping-pong related functions
func (m *Manager) RegisterPongValidator(pubKey ed25519.PublicKey) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.pongValidators[string(pubKey)] = pubKey
}

func (m *Manager) ValidatePongResponse(pingTimestamp uint64, signature, publicKey []byte, currentHeight uint64) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Check if ping exists and hasn't expired
    pingState, exists := m.activePings[pingTimestamp]
    if !exists {
        return ErrInvalidPongResponse
    }

    if currentHeight > pingState.ExpiresAt {
        delete(m.activePings, pingTimestamp)
        return ErrPingTimeout
    }

    // Verify validator is authorized
    if _, ok := m.pongValidators[string(publicKey)]; !ok {
        return ErrUnauthorizedValidator
    }

    // Verify signature
    message := []byte(getSignatureMessage(pingTimestamp))
    if !ed25519.Verify(publicKey, message, signature) {
        return ErrInvalidSignature
    }

    // Remove processed ping
    delete(m.activePings, pingTimestamp)
    return nil
}

// Helper functions
func (m *Manager) cleanupExpiredPings(currentHeight uint64) {
    for timestamp, state := range m.activePings {
        if currentHeight > state.ExpiresAt {
            delete(m.activePings, timestamp)
        }
    }
}

func getSignatureMessage(timestamp uint64) string {
    return codec.EncodeUint64(timestamp)
}

// Stats and monitoring
func (m *Manager) GetStats() EventStats {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return *m.stats
}

func (m *Manager) GetBlockEventCount(height uint64) uint64 {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return uint64(len(m.blockEvents[height]))
}

// Context functions
func (m *Manager) WithContext(ctx context.Context) context.Context {
    return context.WithValue(ctx, managerKey{}, m)
}

func FromContext(ctx context.Context) *Manager {
    if v := ctx.Value(managerKey{}); v != nil {
        if m, ok := v.(*Manager); ok {
            return m
        }
    }
    return nil
}

type managerKey struct{}