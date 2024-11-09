// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "github.com/ava-labs/hypersdk/codec"
    "github.com/near/borsh-go"
)

const (
    // Event validation constants
    MaxEventTypeLength = 64
    MaxEventDataSize   = 1024 * 1024 // 1MB
    DefaultBufferSize  = 100

    // Event types
    EventTypePing = "Ping"
    EventTypePong = "Pong"
)

// Event represents a contract-emitted event
type Event struct {
    Contract    codec.Address `json:"contract"`
    EventType   string       `json:"eventType"`
    Data        []byte       `json:"data"`
    BlockHeight uint64       `json:"blockHeight"`
    Timestamp   uint64       `json:"timestamp"`
}

// Validate checks if the event is valid according to defined rules
func (e *Event) Validate() error {
    if err := ValidateEventType(e.EventType); err != nil {
        return NewValidationError(e.EventType, err)
    }

    if err := ValidateEventData(e.Data); err != nil {
        return NewValidationError(e.EventType, err)
    }

    return nil
}

// Implements custom serialization for events
func (e *Event) Serialize() ([]byte, error) {
    return borsh.Serialize(*e)
}

// Implements custom deserialization for events
func (e *Event) Deserialize(data []byte) error {
    return borsh.Deserialize(e, data)
}

// EventFilter defines criteria for filtering events
type EventFilter struct {
    Contract    *codec.Address
    EventType   *string
    StartHeight *uint64
    EndHeight   *uint64
    StartTime   *uint64
    EndTime     *uint64
}

// Matches checks if an event matches the filter criteria
func (f *EventFilter) Matches(event *Event) bool {
    if f.Contract != nil && *f.Contract != event.Contract {
        return false
    }
    if f.EventType != nil && *f.EventType != event.EventType {
        return false
    }
    if f.StartHeight != nil && event.BlockHeight < *f.StartHeight {
        return false
    }
    if f.EndHeight != nil && event.BlockHeight > *f.EndHeight {
        return false
    }
    if f.StartTime != nil && event.Timestamp < *f.StartTime {
        return false
    }
    if f.EndTime != nil && event.Timestamp > *f.EndTime {
        return false
    }
    return true
}

// EventSubscription represents a subscription to specific event types
type EventSubscription struct {
    ID      uint64
    Filter  EventFilter
    Channel chan Event
}

// EventMetadata contains additional event information
type EventMetadata struct {
    TotalEvents     uint64
    LastBlockHeight uint64
    LastTimestamp   uint64
}

// BlockEvents groups events by block height
type BlockEvents struct {
    Height  uint64
    Events  []Event
    // NEW: Add block-specific metadata
    GasUsed uint64
    Size    uint64
}

// PingEvent represents a ping request
type PingEvent struct {
    Sender    codec.Address
    Timestamp uint64
    Nonce     uint64
}

// PongEvent represents a pong response
type PongEvent struct {
    Responder     codec.Address
    PingTimestamp uint64
    Signature     []byte
    PublicKey     []byte
}

// Validate verifies the pong response
func (p *PongEvent) Validate() error {
    if err := ValidateSignature(p.Signature, p.PublicKey, nil); err != nil {
        return NewPongError("invalid signature", err)
    }
    return nil
}

// EventStats provides statistics about event processing
type EventStats struct {
    TotalProcessed uint64
    TotalFiltered  uint64
    LastProcessed  uint64
    ErrorCount     uint64
}

// EventOptions configures event behavior
type EventOptions struct {
    BufferSize     int
    MaxBlockEvents uint64
    RetryAttempts  int
    RetryDelay     uint64
}

func DefaultEventOptions() *EventOptions {
    return &EventOptions{
        BufferSize:     DefaultBufferSize,
        MaxBlockEvents: 1000,
        RetryAttempts:  3,
        RetryDelay:     1000, // milliseconds
    }
}

// Helper function to create a new event
func NewEvent(contract codec.Address, eventType string, data []byte, height, timestamp uint64) (*Event, error) {
    event := &Event{
        Contract:    contract,
        EventType:   eventType,
        Data:        data,
        BlockHeight: height,
        Timestamp:   timestamp,
    }
    
    if err := event.Validate(); err != nil {
        return nil, err
    }
    
    return event, nil
}

// Helper function to create a new ping event
func NewPingEvent(sender codec.Address, timestamp uint64) *Event {
    return &Event{
        Contract:    sender,
        EventType:   EventTypePing,
        Timestamp:   timestamp,
    }
}

// Helper function to create a new pong event
func NewPongEvent(responder codec.Address, pingTimestamp uint64, signature, publicKey []byte) (*Event, error) {
    pongEvent := &PongEvent{
        Responder:     responder,
        PingTimestamp: pingTimestamp,
        Signature:     signature,
        PublicKey:     publicKey,
    }
    
    if err := pongEvent.Validate(); err != nil {
        return nil, err
    }

    data, err := borsh.Serialize(pongEvent)
    if err != nil {
        return nil, err
    }

    return &Event{
        Contract:    responder,
        EventType:   EventTypePong,
        Data:        data,
        Timestamp:   pingTimestamp,
    }, nil
}