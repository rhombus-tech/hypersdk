// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
)

var (
    ErrOffChainEventDisabled = errors.New("off-chain events are disabled")
    ErrInvalidOffChainEvent  = errors.New("invalid off-chain event")
    ErrEventQueueFull        = errors.New("event queue full")
)

// OffChainEvent extends the base Event type with off-chain specific fields
type OffChainEvent struct {
    Event
    TaskID      uint64
    ExecutionID ids.ID
    EmitTime    time.Time
    Metadata    map[string]string
}

// OffChainEventManager handles events from off-chain workers
type OffChainEventManager struct {
    baseManager *Manager
    log         logging.Logger
    config      *OffChainConfig

    // Event processing
    eventQueue  chan *OffChainEvent
    processed   map[uint64][]*OffChainEvent // blockHeight -> events
    listeners   map[uint64]chan *OffChainEvent
    nextID      uint64

    // Event storage
    storage     OffChainEventStorage
    metrics     *OffChainEventMetrics

    // Synchronization
    lock        sync.RWMutex
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}

// OffChainConfig defines configuration for off-chain event processing
type OffChainConfig struct {
    Enabled        bool
    QueueSize      int
    BatchSize      int
    FlushInterval  time.Duration
    RetentionTime  time.Duration
    StoragePath    string
}

// OffChainEventMetrics tracks event processing statistics
type OffChainEventMetrics struct {
    EventsEmitted   uint64
    EventsProcessed uint64
    EventsDropped   uint64
    ProcessingTime  time.Duration
    QueueHighWater  int
    lock            sync.Mutex
}

// OffChainEventStorage defines interface for event persistence
type OffChainEventStorage interface {
    Store(event *OffChainEvent) error
    Get(executionID ids.ID) (*OffChainEvent, error)
    List(blockHeight uint64) ([]*OffChainEvent, error)
    Delete(executionID ids.ID) error
    Close() error
}

// NewOffChainEventManager creates a new off-chain event manager
func NewOffChainEventManager(
    baseManager *Manager,
    config *OffChainConfig,
    storage OffChainEventStorage,
    log logging.Logger,
) *OffChainEventManager {
    ctx, cancel := context.WithCancel(context.Background())

    return &OffChainEventManager{
        baseManager: baseManager,
        config:      config,
        storage:     storage,
        log:         log,
        eventQueue:  make(chan *OffChainEvent, config.QueueSize),
        processed:   make(map[uint64][]*OffChainEvent),
        listeners:   make(map[uint64]chan *OffChainEvent),
        metrics:     &OffChainEventMetrics{},
        ctx:         ctx,
        cancel:      cancel,
    }
}

// Start begins event processing
func (m *OffChainEventManager) Start() error {
    if !m.config.Enabled {
        return nil
    }

    // Start event processor
    m.wg.Add(1)
    go m.processEvents()

    // Start periodic flusher
    m.wg.Add(1)
    go m.periodicFlush()

    return nil
}

// Stop gracefully shuts down the manager
func (m *OffChainEventManager) Stop() error {
    m.cancel()
    m.wg.Wait()
    return m.storage.Close()
}

// Emit queues an off-chain event for processing
func (m *OffChainEventManager) Emit(event *OffChainEvent) error {
    if !m.config.Enabled {
        return ErrOffChainEventDisabled
    }

    if err := m.validateEvent(event); err != nil {
        return err
    }

    // Assign event ID and timestamp
    event.TaskID = m.nextEventID()
    event.EmitTime = time.Now()

    // Try to queue event
    select {
    case m.eventQueue <- event:
        m.updateMetrics(true, 0)
        return nil
    default:
        m.updateMetrics(false, 1)
        return ErrEventQueueFull
    }
}

// Subscribe creates a new event subscription
func (m *OffChainEventManager) Subscribe() (uint64, chan *OffChainEvent) {
    m.lock.Lock()
    defer m.lock.Unlock()

    id := m.nextID
    m.nextID++

    ch := make(chan *OffChainEvent, m.config.QueueSize)
    m.listeners[id] = ch

    return id, ch
}

// Unsubscribe removes an event subscription
func (m *OffChainEventManager) Unsubscribe(id uint64) {
    m.lock.Lock()
    defer m.lock.Unlock()

    if ch, ok := m.listeners[id]; ok {
        close(ch)
        delete(m.listeners, id)
    }
}

// GetBlockEvents returns events for the specified block
func (m *OffChainEventManager) GetBlockEvents(height uint64) []*OffChainEvent {
    m.lock.RLock()
    defer m.lock.RUnlock()
    return m.processed[height]
}

// Internal event processing

func (m *OffChainEventManager) processEvents() {
    defer m.wg.Done()

    batch := make([]*OffChainEvent, 0, m.config.BatchSize)
    timer := time.NewTimer(m.config.FlushInterval)
    defer timer.Stop()

    for {
        select {
        case event := <-m.eventQueue:
            batch = append(batch, event)
            
            if len(batch) >= m.config.BatchSize {
                m.processBatch(batch)
                batch = batch[:0]
                timer.Reset(m.config.FlushInterval)
            }

        case <-timer.C:
            if len(batch) > 0 {
                m.processBatch(batch)
                batch = batch[:0]
            }
            timer.Reset(m.config.FlushInterval)

        case <-m.ctx.Done():
            if len(batch) > 0 {
                m.processBatch(batch)
            }
            return
        }
    }
}

func (m *OffChainEventManager) processBatch(events []*OffChainEvent) {
    start := time.Now()

    for _, event := range events {
        // Store event
        if err := m.storage.Store(event); err != nil {
            m.log.Error("Failed to store event: %v", err)
            continue
        }

        // Add to processed events
        m.lock.Lock()
        m.processed[event.BlockHeight] = append(
            m.processed[event.BlockHeight],
            event,
        )
        m.lock.Unlock()

        // Notify listeners
        m.notifyListeners(event)

        m.updateMetrics(true, 0)
    }

    m.updateProcessingTime(time.Since(start))
}

func (m *OffChainEventManager) periodicFlush() {
    defer m.wg.Done()

    ticker := time.NewTicker(m.config.FlushInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            m.flushOldEvents()
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *OffChainEventManager) flushOldEvents() {
    m.lock.Lock()
    defer m.lock.Unlock()

    cutoff := uint64(time.Now().Add(-m.config.RetentionTime).Unix())
    
    for height, events := range m.processed {
        if height < cutoff {
            delete(m.processed, height)
            
            // Clean up storage
            for _, event := range events {
                if err := m.storage.Delete(event.ExecutionID); err != nil {
                    m.log.Error("Failed to delete old event: %v", err)
                }
            }
        }
    }
}

func (m *OffChainEventManager) notifyListeners(event *OffChainEvent) {
    m.lock.RLock()
    defer m.lock.RUnlock()

    for _, ch := range m.listeners {
        select {
        case ch <- event:
            // Event delivered
        default:
            // Channel full, skip notification
            m.updateMetrics(false, 1)
        }
    }
}

// Helper methods

func (m *OffChainEventManager) validateEvent(event *OffChainEvent) error {
    if event == nil {
        return ErrInvalidOffChainEvent
    }

    if event.Contract == codec.EmptyAddress {
        return fmt.Errorf("%w: invalid contract address", ErrInvalidOffChainEvent)
    }

    if len(event.EventType) == 0 {
        return fmt.Errorf("%w: empty event type", ErrInvalidOffChainEvent)
    }

    return nil
}

func (m *OffChainEventManager) nextEventID() uint64 {
    m.lock.Lock()
    defer m.lock.Unlock()
    
    id := m.nextID
    m.nextID++
    return id
}

// Metrics management

func (m *OffChainEventManager) updateMetrics(processed bool, dropped uint64) {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()

    m.metrics.EventsEmitted++
    if processed {
        m.metrics.EventsProcessed++
    }
    m.metrics.EventsDropped += dropped

    queueLen := len(m.eventQueue)
    if queueLen > m.metrics.QueueHighWater {
        m.metrics.QueueHighWater = queueLen
    }
}

func (m *OffChainEventManager) updateProcessingTime(duration time.Duration) {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()
    m.metrics.ProcessingTime += duration
}

// GetMetrics returns current event processing metrics
func (m *OffChainEventManager) GetMetrics() OffChainEventMetrics {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()
    return *m.metrics
}

// Helper function to create a new off-chain event
func NewOffChainEvent(
    contract codec.Address,
    eventType string,
    data []byte,
    blockHeight uint64,
    executionID ids.ID,
    metadata map[string]string,
) *OffChainEvent {
    return &OffChainEvent{
        Event: Event{
            Contract:    contract,
            EventType:   eventType,
            Data:       data,
            BlockHeight: blockHeight,
            Timestamp:   uint64(time.Now().Unix()),
        },
        ExecutionID: executionID,
        EmitTime:    time.Now(),
        Metadata:    metadata,
    }
}