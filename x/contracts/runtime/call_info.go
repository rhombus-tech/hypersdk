// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime/events"
    "github.com/ava-labs/hypersdk/runtime/timechain"
)

type CallInfo struct {
    State        StateManager
    Actor        codec.Address
    Contract     codec.Address
    FunctionName string
    Params       []byte
    Fuel         uint64
    Height       uint64
    ActionID     ids.ID
    Timestamp    uint64
    Value        uint64

    // Instance-specific fields
    inst         *ContractInstance
    events       []events.Event
    
    // TimeChain fields
    TimeProof    *timechain.RoughtimeProof
    SequenceNum  uint64
}

func (c *CallInfo) RemainingFuel() uint64 {
    return c.Fuel
}

func (c *CallInfo) ConsumeFuel(amount uint64) error {
    if amount > c.Fuel {
        return ErrOutOfFuel
    }
    c.Fuel -= amount
    return nil
}

func (c *CallInfo) AddFuel(amount uint64) {
    c.Fuel += amount
}