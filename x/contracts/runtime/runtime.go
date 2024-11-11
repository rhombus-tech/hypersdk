// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "errors"
    "fmt"
    "reflect"

    "github.com/ava-labs/avalanchego/cache"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/bytecodealliance/wasmtime-go/v25"

    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime/events"
    "github.com/ava-labs/hypersdk/state"
)

var (
    callInfoTypeInfo = reflect.TypeOf(CallInfo{})
    errCannotOverwrite = errors.New("trying to overwrite set field")
)

type WasmRuntime struct {
    log    logging.Logger
    engine *wasmtime.Engine
    cfg    *Config

    contractCache cache.Cacher[string, *wasmtime.Module]

    callerInfo map[uintptr]*CallInfo
    linker     *wasmtime.Linker

    // Preserved from original
    eventManager *events.Manager
}

// Original interfaces preserved
type StateManager interface {
    BalanceManager
    ContractManager
}

type BalanceManager interface {
    GetBalance(ctx context.Context, address codec.Address) (uint64, error)
    TransferBalance(ctx context.Context, from codec.Address, to codec.Address, amount uint64) error
}

type ContractManager interface {
    GetContractState(address codec.Address) state.Mutable
    GetAccountContract(ctx context.Context, account codec.Address) (ContractID, error)
    GetContractBytes(ctx context.Context, contractID ContractID) ([]byte, error)
    NewAccountWithContract(ctx context.Context, contractID ContractID, accountCreationData []byte) (codec.Address, error)
    SetAccountContract(ctx context.Context, account codec.Address, contractID ContractID) error
    SetContractBytes(ctx context.Context, contractID ContractID, contractBytes []byte) error
}

// Preserved original NewRuntime with event initialization
func NewRuntime(cfg *Config, log logging.Logger) *WasmRuntime {
    var eventManager *events.Manager
    if cfg.EventConfig != nil && cfg.EventConfig.Enabled {
        eventManager = events.NewManager(cfg.EventConfig, nil)
    }

    hostImports := NewImports()

    runtime := &WasmRuntime{
        log:        log,
        cfg:        cfg,
        engine:     wasmtime.NewEngineWithConfig(cfg.wasmConfig),
        callerInfo: map[uintptr]*CallInfo{},
        contractCache: cache.NewSizedLRU(cfg.ContractCacheSize, func(id string, mod *wasmtime.Module) int {
            bytes, err := mod.Serialize()
            if err != nil {
                panic(err)
            }
            return len(id) + len(bytes)
        }),
        eventManager: eventManager,
    }

    hostImports.AddModule(NewLogModule())
    hostImports.AddModule(NewBalanceModule())
    hostImports.AddModule(NewStateAccessModule())
    hostImports.AddModule(NewContractModule(runtime))

    if eventManager != nil {
        hostImports.AddModule(events.NewEventModule(eventManager))
    }

    linker, err := hostImports.createLinker(runtime)
    if err != nil {
        panic(err)
    }

    runtime.linker = linker

    return runtime
}

// Preserved original Events accessor
func (r *WasmRuntime) Events() *events.Manager {
    return r.eventManager
}

// Original CallInfo preserved with events
type CallInfo struct {
    State        StateManager
    Actor        codec.Address
    FunctionName string
    Contract     codec.Address
    Params       []byte
    Fuel         uint64
    Height       uint64
    Timestamp    uint64
    Value        uint64
    inst         *ContractInstance
    events       []events.Event
}

// Original WithDefaults preserved
func (r *WasmRuntime) WithDefaults(callInfo CallInfo) CallContext {
    if r.eventManager != nil {
        callInfo.events = make([]events.Event, 0)
    }
    return CallContext{r: r, defaultCallInfo: callInfo}
}

func (r *WasmRuntime) getModule(ctx context.Context, callInfo *CallInfo, id []byte) (*wasmtime.Module, error) {
    if mod, ok := r.contractCache.Get(string(id)); ok {
        return mod, nil
    }
    contractBytes, err := callInfo.State.GetContractBytes(ctx, id)
    if err != nil {
        return nil, err
    }
    mod, err := wasmtime.NewModule(r.engine, contractBytes)
    if err != nil {
        return nil, err
    }
    r.contractCache.Put(string(id), mod)
    return mod, nil
}

// Updated CallContract to use chain.Result while preserving event functionality
func (r *WasmRuntime) CallContract(ctx context.Context, callInfo *CallInfo) (*chain.Result, error) {
    contractID, err := callInfo.State.GetAccountContract(ctx, callInfo.Contract)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }

    // Preserved: Add event context
    if r.eventManager != nil {
        ctx = r.eventManager.WithContext(ctx)
    }

    contractModule, err := r.getModule(ctx, callInfo, contractID)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }

    inst, err := r.getInstance(contractModule)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }
    callInfo.inst = inst

    r.setCallInfo(inst.store, callInfo)
    defer r.deleteCallInfo(inst.store)

    // Execute contract and create result
    output, err := inst.call(ctx, callInfo)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }

    result := &chain.Result{
        Success: true,
        Outputs: make([][]byte, 0),
    }

    if len(output) > 0 {
        result.Outputs = append(result.Outputs, output)
    }

    // Preserved: Process events after successful execution
    if r.eventManager != nil && len(callInfo.events) > 0 {
        for _, evt := range callInfo.events {
            result, err = r.eventManager.EmitWithResult(evt, result)
            if err != nil {
                return &chain.Result{
                    Success: false,
                    Error:   []byte(err.Error()),
                }, nil
            }
        }
    }

    return result, nil
}

// Original instance management preserved
func (r *WasmRuntime) getInstance(contractModule *wasmtime.Module) (*ContractInstance, error) {
    store := wasmtime.NewStore(r.engine)
    store.SetEpochDeadline(1)
    inst, err := r.linker.Instantiate(store, contractModule)
    if err != nil {
        return nil, err
    }
    return &ContractInstance{inst: inst, store: store}, nil
}

// Original helper functions preserved
func toMapKey(storeLike wasmtime.Storelike) uintptr {
    return reflect.ValueOf(storeLike.Context()).Pointer()
}

func (r *WasmRuntime) setCallInfo(storeLike wasmtime.Storelike, info *CallInfo) {
    r.callerInfo[toMapKey(storeLike)] = info
}

func (r *WasmRuntime) getCallInfo(storeLike wasmtime.Storelike) *CallInfo {
    return r.callerInfo[toMapKey(storeLike)]
}

func (r *WasmRuntime) deleteCallInfo(storeLike wasmtime.Storelike) {
    delete(r.callerInfo, toMapKey(storeLike))
}

// Original block handling hooks preserved
func (r *WasmRuntime) OnBlockCommitted(height uint64, timestamp uint64) error {
    if r.eventManager != nil {
        return r.eventManager.OnBlockCommitted(height, timestamp)
    }
    return nil
}

func (r *WasmRuntime) OnBlockRolledBack(height uint64) error {
    if r.eventManager != nil {
        return r.eventManager.OnBlockRolledBack(height)
    }
    return nil
}