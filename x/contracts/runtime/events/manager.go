// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/near/borsh-go"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
)

type PingState struct {
	Timestamp uint64
	Sender    codec.Address
	ExpiresAt uint64
}

// Manager handles event emission, subscription, and validation
type Manager struct {
	// Protected by mu
	mu            sync.RWMutex
	events        []Event
	blockEvents   map[uint64][]Event
	listeners     []chan Event
	subscriptions map[uint64]*EventSubscription
	nextSubID     uint64

	// Block management
	currentBlock  uint64
	currentTime   uint64
	eventsInBlock uint64

	// Configuration
	config  *Config
	options *EventOptions

	// Stats
	stats EventStats

	// Ping-pong state
	activePings    map[uint64]*PingState
	pongValidators map[string]ed25519.PublicKey

	// Event processing
	eventQueue chan Event
	done       chan struct{}
	wg         sync.WaitGroup
}

// NewManager creates a new event manager instance
func NewManager(cfg *Config, opts *EventOptions) *Manager {
	if cfg == nil {
		cfg = DefaultConfig()
	}
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
		eventQueue:     make(chan Event, opts.BufferSize),
		done:          make(chan struct{}),
		listeners:      make([]chan Event, 0),
	}
}

// EmitWithResult processes an event and includes it in the chain result
func (m *Manager) EmitWithResult(evt Event, result *chain.Result) (*chain.Result, error) {
	if !m.config.Enabled {
		return result, nil
	}

	if err := m.validateEvent(evt); err != nil {
		m.stats.ErrorCount++
		return result, err
	}

	// Serialize event
	eventData, err := borsh.Serialize(evt)
	if err != nil {
		m.stats.ErrorCount++
		return result, err
	}

	// Add event data to result outputs
	result.Outputs = append(result.Outputs, eventData)

	// Process event
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		for attempt := 0; attempt < m.options.RetryAttempts; attempt++ {
			select {
			case m.eventQueue <- evt:
				return
			case <-m.done:
				return
			default:
				if attempt < m.options.RetryAttempts-1 {
					time.Sleep(time.Duration(m.options.RetryDelay) * time.Millisecond)
					continue
				}
				m.stats.DroppedEvents++
				return
			}
		}
	}()

	return result, nil
}

// Emit sends an event without attaching to a result
func (m *Manager) Emit(evt Event) {
	if !m.config.Enabled {
		return
	}

	if err := m.validateEvent(evt); err != nil {
		m.stats.ErrorCount++
		return
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		select {
		case m.eventQueue <- evt:
		case <-m.done:
		}
	}()
}

// Background event processor
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

// HandleEvent processes a single event
func (m *Manager) handleEvent(evt Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add to storage
	m.events = append(m.events, evt)
	m.blockEvents[evt.BlockHeight] = append(m.blockEvents[evt.BlockHeight], evt)
	m.eventsInBlock++

	// Update stats
	m.stats.TotalProcessed++
	m.stats.LastProcessed = uint64(time.Now().Unix())

	// Notify listeners
	for _, listener := range m.listeners {
		m.notifyListener(listener, evt)
	}

	// Notify subscribers
	for _, sub := range m.subscriptions {
		if sub.Filter.Matches(&evt) {
			m.stats.TotalFiltered++
			m.notifySubscriber(sub, evt)
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

// GetBlockEvents returns events for a specific block
func (m *Manager) GetBlockEvents(height uint64) []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blockEvents[height]
}

// GetEventsResult returns a chain.Result containing events for a block
func (m *Manager) GetEventsResult(height uint64) *chain.Result {
	events := m.GetBlockEvents(height)
	if len(events) == 0 {
		return &chain.Result{Success: true}
	}

	result := &chain.Result{
		Success: true,
		Outputs: make([][]byte, 0, len(events)),
	}

	for _, evt := range events {
		if eventData, err := borsh.Serialize(evt); err == nil {
			result.Outputs = append(result.Outputs, eventData)
		}
	}

	return result
}

// Block handling
func (m *Manager) OnBlockCommitted(height uint64, timestamp uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentBlock = height
	m.currentTime = timestamp
	m.eventsInBlock = 0

	m.cleanupExpiredPings(height)

	return nil
}

func (m *Manager) OnBlockRolledBack(height uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if height >= m.currentBlock {
		return ErrFutureBlock
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

// Validation
func (m *Manager) validateEvent(evt Event) error {
	if err := ValidateEventType(evt.EventType); err != nil {
		return NewValidationError(evt.EventType, err)
	}

	if err := ValidateEventData(evt.Data); err != nil {
		return NewValidationError(evt.EventType, err)
	}

	if err := m.validateBlockLimit(evt.BlockHeight); err != nil {
		return err
	}

	return nil
}

func (m *Manager) validateBlockLimit(height uint64) error {
	if m.eventsInBlock >= m.config.MaxBlockSize {
		return ErrTooManyEvents
	}
	return nil
}

// Ping-pong protocol
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
	message := []byte(codec.EncodeUint64(pingTimestamp))
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrInvalidSignature
	}

	// Remove processed ping
	delete(m.activePings, pingTimestamp)
	return nil
}

// Notification helpers
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

func (m *Manager) notifySubscriber(sub *EventSubscription, evt Event) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		select {
		case sub.Channel <- evt:
			// Event delivered
		case <-m.done:
			// Manager is shutting down
		default:
			// Channel full, drop event
			m.stats.DroppedEvents++
		}
	}()
}

func (m *Manager) cleanupExpiredPings(currentHeight uint64) {
	for timestamp, state := range m.activePings {
		if currentHeight > state.ExpiresAt {
			delete(m.activePings, timestamp)
		}
	}
}

// Stats and monitoring
func (m *Manager) GetStats() EventStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// Shutdown gracefully stops the event manager
func (m *Manager) Shutdown(ctx context.Context) error {
	close(m.done)

	// Close all existing subscription channels
	m.mu.Lock()
	for _, sub := range m.subscriptions {
		close(sub.Channel)
	}
	m.subscriptions = make(map[uint64]*EventSubscription)
	m.mu.Unlock()

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

// Context methods
type managerKey struct{}

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