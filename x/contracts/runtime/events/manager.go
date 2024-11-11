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

    // NEW: Event processing channels and synchronization
    eventQueue      chan Event
    done            chan struct{}
    wg              sync.WaitGroup
}

type PingState struct {
    Timestamp uint64
    Sender    codec.Address
    ExpiresAt uint64
}

// MODIFIED: Updated to initialize event processing
func NewManager(cfg *Config, opts *EventOptions) *Manager {
    if opts == nil {
        opts = DefaultEventOptions()
    }
    
    m := &Manager{
        blockEvents:    make(map[uint64][]Event),
        subscriptions:  make(map[uint64]*EventSubscription),
        activePings:    make(map[uint64]*PingState),
        pongValidators: make(map[string]ed25519.PublicKey),
        config:         cfg,
        options:        opts,
        stats:         &EventStats{},
        // NEW: Initialize event processing channels
        eventQueue:    make(chan Event, opts.InitialQueueSize),
        done:         make(chan struct{}),
    }

    // NEW: Start event processor
    go m.processEvents()

    return m
}

// MODIFIED: Emit is now non-blocking
func (m *Manager) Emit(evt Event) {
    if !m.config.Enabled {
        return
    }

    m.wg.Add(1)
    go func() {
        defer m.wg.Done()
        select {
        case m.eventQueue <- evt:
            // Event queued successfully
        case <-m.done:
            // Manager is shutting down
        }
    }()
}

// NEW: Background event processor
func (m *Manager) processEvents() {
    for {
        select {
        case evt := <-m.eventQueue:
            m.handleEvent(evt)
        case <-m.done:
            return
        }
    }
}

// NEW: Handle individual event
func (m *Manager) handleEvent(evt Event) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Add to main events list
    m.events = append(m.events, evt)
    
    // Add to block events
    m.blockEvents[evt.BlockHeight] = append(m.blockEvents[evt.BlockHeight], evt)
    m.eventsInBlock++
    
    // Update stats
    m.stats.TotalProcessed++
    m.stats.LastProcessed = uint64(time.Now().Unix())

    // Notify subscribers asynchronously
    for _, listener := range m.listeners {
        m.notifyListener(listener, evt)
    }

    // Process subscriptions asynchronously
    for _, sub := range m.subscriptions {
        if sub.Filter.Matches(&evt) {
            m.notifySubscriber(sub, evt)
        }
    }
}

// NEW: Asynchronous listener notification
func (m *Manager) notifyListener(listener chan Event, evt Event) {
    m.wg.Add(1)
    go func() {
        defer m.wg.Done()
        select {
        case listener <- evt:
            // Event delivered
        case <-m.done:
            // Manager is shutting down
        }
    }()
}

// NEW: Asynchronous subscriber notification
func (m *Manager) notifySubscriber(sub *EventSubscription, evt Event) {
    m.wg.Add(1)
    go func() {
        defer m.wg.Done()
        select {
        case sub.Channel <- evt:
            // Event delivered
        case <-m.done:
            // Manager is shutting down
        }
    }()
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

// NEW: Graceful shutdown
func (m *Manager) Shutdown(ctx context.Context) error {
    close(m.done)
    
    // Wait for all event processing with timeout
    done := make(chan struct{})
    go func() {
        m.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}