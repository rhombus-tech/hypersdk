// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
    "context"

    "github.com/ava-labs/hypersdk/chain"
)

// TransactionPool interface for submitting transactions
type TransactionPool interface {
    AddLocal(tx *chain.Transaction) error
}

// StateManager interface for state access
type StateManager interface {
    GetValue(ctx context.Context, key []byte) ([]byte, error)
    SetValue(ctx context.Context, key []byte, value []byte) error
}

// Add coordination interfaces
type Coordinator interface {
    CoordinateTask(task Task) error
    RegisterWorker(worker *Worker)
    UnregisterWorker(workerID WorkerID)
}

type SecureChannelProvider interface {
    EstablishSecureChannel(partnerID WorkerID) (*SecureChannel, error)
    SendSecure(channel *SecureChannel, data []byte) error
    ReceiveSecure(channel *SecureChannel) ([]byte, error)
}