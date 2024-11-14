// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import "C"

import (
	"context"
	"errors"
	"reflect"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/bytecodealliance/wasmtime-go/v25"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/runtime/events"
)

const (
	AllocName  = "alloc"
	MemoryName = "memory"
)

var (
	callInfoTypeInfo = reflect.TypeOf(CallInfo{})
)

type ContractID []byte

type Context struct {
	Contract  codec.Address
	Actor     codec.Address
	Height    uint64
	Timestamp uint64
	ActionID  ids.ID
}

type CallInfo struct {
	// the state that the contract will run against
	State StateManager

	// the address that originated the initial contract call
	Actor codec.Address

	// the name of the function within the contract that is being called
	FunctionName string

	Contract codec.Address

	// the serialized parameters that will be passed to the called function
	Params []byte

	// the maximum amount of fuel allowed to be consumed by wasm for this call
	Fuel uint64

	// the height of the chain that this call was made from
	Height uint64

	// the timestamp of the chain at the time this call was made
	Timestamp uint64

	// the action id that triggered this call
	ActionID ids.ID

	Value uint64

	inst *ContractInstance

	// Event storage
    events []events.Event 
}

func (c *CallInfo) RemainingFuel() uint64 {
	remaining, err := c.inst.store.GetFuel()
	if err != nil {
		return c.Fuel
	}
	return remaining
}

func (c *CallInfo) AddFuel(fuel uint64) {
	// only errors if fuel isn't enable, which it always will be
	remaining, err := c.inst.store.GetFuel()
	if err != nil {
		return
	}
	_ = c.inst.store.SetFuel(remaining + fuel)
}

func (c *CallInfo) ConsumeFuel(fuel uint64) error {
	remaining, err := c.inst.store.GetFuel()
	if err != nil {
		return err
	}

	if remaining < fuel {
		return errors.New("out of fuel")
	}

	return c.inst.store.SetFuel(remaining - fuel)
}

type ContractInstance struct {
	inst   *wasmtime.Instance
	store  *wasmtime.Store
	result []byte
}

func (p *ContractInstance) call(ctx context.Context, callInfo *CallInfo) (*chain.Result, error) {
	remaining, err := p.store.GetFuel()
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	if err := p.store.SetFuel(remaining + callInfo.Fuel); err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	if callInfo.Value > 0 {
		if err := callInfo.State.TransferBalance(ctx, callInfo.Actor, callInfo.Contract, callInfo.Value); err != nil {
			return &chain.Result{
				Success: false,
				Error:   []byte("insufficient balance"),
			}, nil
		}
	}

	// create the contract context
	contractCtx := Context{
		Contract:  callInfo.Contract,
		Actor:     callInfo.Actor,
		Height:    callInfo.Height,
		Timestamp: callInfo.Timestamp,
		ActionID:  callInfo.ActionID,
	}
	paramsBytes, err := Serialize(contractCtx)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}
	paramsBytes = append(paramsBytes, callInfo.Params...)

	// copy params into store linear memory
	paramsOffset, err := p.writeToMemory(paramsBytes)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	function := p.inst.GetFunc(p.store, callInfo.FunctionName)
	if function == nil {
		return &chain.Result{
			Success: false,
			Error:   []byte("this function does not exist"),
		}, nil
	}

	_, err = function.Call(p.store, paramsOffset)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
	}

	// Return successful result with function output
	return &chain.Result{
		Success: true,
		Outputs: [][]byte{p.result},
	}, nil
}

func (p *ContractInstance) writeToMemory(data []byte) (int32, error) {
	allocFn := p.inst.GetExport(p.store, AllocName).Func()
	contractMemory := p.inst.GetExport(p.store, MemoryName).Memory()
	dataOffsetIntf, err := allocFn.Call(p.store, int32(len(data)))
	if err != nil {
		return 0, err
	}
	dataOffset := dataOffsetIntf.(int32)
	linearMem := contractMemory.UnsafeData(p.store)
	copy(linearMem[dataOffset:], data)
	return dataOffset, nil
}

// Helper function to wrap errors in chain.Result
func wrapError(err error) *chain.Result {
	return &chain.Result{
		Success: false,
		Error:   []byte(err.Error()),
	}
}

// Helper function to create successful result
func createSingleOutputResult(output []byte) *chain.Result {
    if output == nil {
        output = []byte{}
    }
    return &chain.Result{
        Success: true,
        Outputs: [][]byte{output},
    }
}