// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import "time"

type CoordinationOptions struct {
    // Add to existing WorkerConfig
    CoordinationEnabled   bool
    CoordinationTimeout   time.Duration
    SecureChannelEnabled  bool
    MinWorkers           int
    MaxCoordinationRetries int
}

func DefaultCoordinationOptions() *CoordinationOptions {
    return &CoordinationOptions{
        CoordinationEnabled:     true,
        CoordinationTimeout:     30 * time.Second,
        SecureChannelEnabled:    true,
        MinWorkers:             2,
        MaxCoordinationRetries: 3,
    }
}