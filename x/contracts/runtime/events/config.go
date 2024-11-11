// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "time"
)

const (
    DefaultMaxBlockSize     = uint64(1000)
    DefaultInitialQueueSize = 10000
    DefaultBufferSize       = 100
    DefaultShutdownTimeout  = 5 * time.Second
)

// Config defines event system configuration
type Config struct {
    // Enabled determines if event processing is active
    Enabled bool `json:"enabled"`

    // MaxBlockSize is the maximum number of events allowed per block
    MaxBlockSize uint64 `json:"maxBlockSize"`

    // InitialQueueSize is the starting size of the event processing queue
    InitialQueueSize int `json:"initialQueueSize"`

    // BufferSize is the size of individual subscription channels
    BufferSize int `json:"bufferSize"`

    // PongValidators contains authorized validator public keys
    PongValidators [][32]byte `json:"pongValidators"`
}

func DefaultConfig() *Config {
    return &Config{
        Enabled:          true,
        MaxBlockSize:     DefaultMaxBlockSize,
        InitialQueueSize: DefaultInitialQueueSize,
        BufferSize:       DefaultBufferSize,
        PongValidators:   make([][32]byte, 0),
    }
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
    if c.MaxBlockSize == 0 {
        return ErrInvalidBlockSize
    }
    if c.InitialQueueSize == 0 {
        return ErrInvalidQueueSize
    }
    if c.BufferSize == 0 {
        return ErrInvalidBufferSize
    }
    return nil
}