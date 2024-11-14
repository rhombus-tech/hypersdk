// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package modules

import (
    "github.com/ava-labs/hypersdk/codec"
    "github.com/bytecodealliance/wasmtime-go/v25"
)

// Module defines the interface for a WASM module
type Module interface {
    // Name returns the module's name
    Name() string
    
    // GetFunctions returns the module's host functions
    GetFunctions() map[string]HostFunction
}

// HostFunction represents a function that can be called from WASM
type HostFunction struct {
    // FuelCost is the amount of fuel consumed by this function
    FuelCost uint64
    
    // Function is the actual implementation
    Function HostFunctionType
}

// HostFunctionType defines the interface for function implementations
type HostFunctionType interface {
    // WasmType returns the WASM function type signature
    WasmType() *wasmtime.FuncType
    
    // Call executes the function with given context and parameters
    Call(*CallInfo, *wasmtime.Caller, []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap)
}

// Function types for different parameter patterns
type Function[T any, U any] func(*CallInfo, T) (U, error)
type FunctionNoInput[T any] func(*CallInfo) (T, error)
type FunctionNoOutput[T any] func(*CallInfo, T) error

// WasmType implementations for different function types
func (Function[T, U]) WasmType() *wasmtime.FuncType {
    return wasmtime.NewFuncType(
        []*wasmtime.ValType{TypeI32, TypeI32},
        []*wasmtime.ValType{TypeI32},
    )
}

func (FunctionNoInput[T]) WasmType() *wasmtime.FuncType {
    return wasmtime.NewFuncType(
        []*wasmtime.ValType{},
        []*wasmtime.ValType{TypeI32},
    )
}

func (FunctionNoOutput[T]) WasmType() *wasmtime.FuncType {
    return wasmtime.NewFuncType(
        []*wasmtime.ValType{TypeI32, TypeI32},
        []*wasmtime.ValType{},
    )
}

// Common WASM types
var (
    TypeI32 = wasmtime.NewValType(wasmtime.KindI32)
    TypeI64 = wasmtime.NewValType(wasmtime.KindI64)
)

// Memory operations
const (
    MemoryName = "memory"
    AllocName  = "alloc"
)

// ContractInstance represents an instantiated WASM contract
type ContractInstance struct {
    Instance *wasmtime.Instance
    Store    *wasmtime.Store
}

// StateManager interface for accessing contract state
type StateManager interface {
    GetValue(key []byte) ([]byte, error)
    SetValue(key []byte, value []byte) error
    DeleteValue(key []byte) error
}

// Helper functions for parameter handling
func getInputFromMemory[T any](caller *wasmtime.Caller, vals []wasmtime.Val) (*T, error) {
    offset := vals[0].I32()
    length := vals[1].I32()

    if offset == 0 || length == 0 {
        return new(T), nil
    }

    memory := caller.GetExport(MemoryName).Memory()
    data := memory.UnsafeData(caller)[offset : offset+length]
    
    return Deserialize[T](data)
}

func writeOutputToMemory[T any](callInfo *CallInfo, results T, err error) ([]wasmtime.Val, *wasmtime.Trap) {
    if err != nil {
        return NilResult, ConvertToTrap(err)
    }

    data, err := Serialize(results)
    if data == nil || err != nil {
        return NilResult, ConvertToTrap(err)
    }

    offset, err := allocateMemory(callInfo.Instance, data)
    if err != nil {
        return NilResult, ConvertToTrap(err)
    }

    return []wasmtime.Val{wasmtime.ValI32(offset)}, nil
}

// Helper for memory allocation
func allocateMemory(instance *ContractInstance, data []byte) (int32, error) {
    alloc := instance.Instance.GetExport(instance.Store, AllocName).Func()
    memory := instance.Instance.GetExport(instance.Store, MemoryName).Memory()

    result, err := alloc.Call(instance.Store, int32(len(data)))
    if err != nil {
        return 0, err
    }

    offset := result.(int32)
    copy(memory.UnsafeData(instance.Store)[offset:], data)
    
    return offset, nil
}

// Common constants and variables
var (
    NilResult = []wasmtime.Val{wasmtime.ValI32(0)}
)

// Error conversion helpers
func ConvertToTrap(err error) *wasmtime.Trap {
    if err == nil {
        return nil
    }
    return wasmtime.NewTrap(err.Error())
}