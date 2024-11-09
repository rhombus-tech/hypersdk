// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "errors"
    "fmt"
)

var (
    // Event validation errors
    ErrEventTooLarge     = errors.New("event data exceeds maximum size")
    ErrInvalidEventType  = errors.New("invalid event type")
    ErrTooManyEvents     = errors.New("too many events in block")
    ErrEventsDisabled    = errors.New("events are disabled")
    ErrFutureBlock       = errors.New("event references future block")

    // Ping-pong related errors
    ErrInvalidSignature       = errors.New("invalid signature")
    ErrInvalidPublicKey      = errors.New("invalid public key")
    ErrUnauthorizedValidator = errors.New("unauthorized validator")
    ErrPingTimeout          = errors.New("ping timeout exceeded")
    ErrInvalidPongResponse  = errors.New("invalid pong response")
)

// ValidationError wraps event validation errors with context
type ValidationError struct {
    EventType string
    Reason    error
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("event validation failed for type '%s': %v", e.EventType, e.Reason)
}

func NewValidationError(eventType string, reason error) *ValidationError {
    return &ValidationError{
        EventType: eventType,
        Reason:    reason,
    }
}

// BlockError represents errors related to block events
type BlockError struct {
    Height uint64
    Reason error
}

func (e *BlockError) Error() string {
    return fmt.Sprintf("block event error at height %d: %v", e.Height, e.Reason)
}

func NewBlockError(height uint64, reason error) *BlockError {
    return &BlockError{
        Height: height,
        Reason: reason,
    }
}

// PingPongError represents errors in ping-pong protocol
type PingPongError struct {
    Type    string // "ping" or "pong"
    Details string
    Reason  error
}

func (e *PingPongError) Error() string {
    return fmt.Sprintf("%s error: %s - %v", e.Type, e.Details, e.Reason)
}

func NewPingError(details string, reason error) *PingPongError {
    return &PingPongError{
        Type:    "ping",
        Details: details,
        Reason:  reason,
    }
}

func NewPongError(details string, reason error) *PingPongError {
    return &PingPongError{
        Type:    "pong",
        Details: details,
        Reason:  reason,
    }
}

// Validation helpers
func ValidateEventType(eventType string) error {
    if len(eventType) == 0 {
        return fmt.Errorf("%w: empty event type", ErrInvalidEventType)
    }
    if len(eventType) > MaxEventTypeLength {
        return fmt.Errorf("%w: length exceeds %d", ErrInvalidEventType, MaxEventTypeLength)
    }
    return nil
}

func ValidateEventData(data []byte) error {
    if len(data) > MaxEventDataSize {
        return fmt.Errorf("%w: size %d exceeds maximum %d", ErrEventTooLarge, len(data), MaxEventDataSize)
    }
    return nil
}

func ValidateBlockSize(currentSize, maxSize uint64) error {
    if currentSize >= maxSize {
        return fmt.Errorf("%w: current size %d exceeds maximum %d", ErrTooManyEvents, currentSize, maxSize)
    }
    return nil
}

func ValidateSignature(signature, publicKey []byte, message []byte) error {
    if len(signature) == 0 {
        return fmt.Errorf("%w: empty signature", ErrInvalidSignature)
    }
    if len(publicKey) == 0 {
        return fmt.Errorf("%w: empty public key", ErrInvalidPublicKey)
    }
    // Additional signature validation logic would go here
    return nil
}

// Helper for checking if an error is of a specific type
func IsEventError(err error) bool {
    switch {
    case errors.Is(err, ErrEventTooLarge):
        return true
    case errors.Is(err, ErrInvalidEventType):
        return true
    case errors.Is(err, ErrTooManyEvents):
        return true
    case errors.Is(err, ErrEventsDisabled):
        return true
    case errors.Is(err, ErrFutureBlock):
        return true
    default:
        return false
    }
}

func IsPingPongError(err error) bool {
    switch {
    case errors.Is(err, ErrInvalidSignature):
        return true
    case errors.Is(err, ErrInvalidPublicKey):
        return true
    case errors.Is(err, ErrUnauthorizedValidator):
        return true
    case errors.Is(err, ErrPingTimeout):
        return true
    case errors.Is(err, ErrInvalidPongResponse):
        return true
    default:
        return false
    }
}