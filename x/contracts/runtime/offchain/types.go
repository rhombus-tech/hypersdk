// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

type TaskID string

type Task struct {
    ID            TaskID
    Contract      codec.Address
    Function      string
    Params        []byte
    BlockContext  BlockContext
    Priority      uint8
    RetryCount    int
    Timeout       time.Duration
    // Add coordination fields
    RequiresCoordination bool
    CoordinationConfig  *CoordinationConfig
}

type CoordinationConfig struct {
    SecureChannel bool
    MinWorkers    int
    MaxAttempts   int
    RetryDelay    time.Duration
}

type BlockContext struct {
    Height      uint64
    Hash        ids.ID
    ParentHash  ids.ID
    Timestamp   uint64
}

// Add coordination result types
type CoordinatedResult struct {
    TaskID    TaskID
    WorkerID  WorkerID
    Output    []byte
    Error     error
}