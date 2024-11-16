// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "errors"
    "reflect"
    "time"

    "github.com/ava-labs/avalanchego/cache"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/bytecodealliance/wasmtime-go/v25"
    
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime/events"
    "github.com/ava-labs/hypersdk/runtime/timechain"
)

var (
    errCannotOverwrite = errors.New("trying to overwrite set field")
)

type WasmRuntime struct {
    log           logging.Logger
    engine        *wasmtime.Engine
    cfg           *Config
    contractCache cache.Cacher[string, *wasmtime.Module]
    callerInfo    map[uintptr]*CallInfo
    linker        *wasmtime.Linker

    // Event management
    eventManager  *events.Manager

    // TimeChain integration
    timechain     *timechain.TimeChain
}

// NewRuntime creates a new WASM runtime instance
func NewRuntime(cfg *Config, log logging.Logger) *WasmRuntime {
    var eventManager *events.Manager
    if cfg.EventConfig != nil && cfg.EventConfig.Enabled {
        eventManager = events.NewManager(cfg.EventConfig, nil)
    }

    hostImports := NewImports()

    runtime := &WasmRuntime{
        log:           log,
        cfg:           cfg,
        engine:        wasmtime.NewEngineWithConfig(cfg.wasmConfig),
        callerInfo:    map[uintptr]*CallInfo{},
        contractCache: cache.NewSizedLRU(cfg.ContractCacheSize, func(id string, mod *wasmtime.Module) int {
            bytes, err := mod.Serialize()
            if err != nil {
                panic(err)
            }
            return len(id) + len(bytes)
        }),
        eventManager: eventManager,
    }

    // Initialize TimeChain if enabled
if cfg.TimeChain != nil && cfg.TimeChain.Enabled {
    roughtime := timechain.NewRoughtimeClient(cfg.TimeChain.MinServers)
    
    // Initialize verifier with configured tolerance
    verifier := timechain.NewVerifier(
        cfg.TimeChain.VerificationTolerance,
        cfg.TimeChain.WindowDuration, // Use as maxAge
        roughtime,
    )
    
    sequence := timechain.NewTimeSequence(
        roughtime,
        cfg.TimeChain.WindowDuration,
        verifier,
    )
    
    runtime.timechain = &timechain.TimeChain{
        Sequence:    sequence,
        MerkleTree:  timechain.NewTimeMerkleTree(),
        Roughtime:   roughtime,
        Verifier:    verifier,
    }

    // Start background time synchronization if configured
    if err := runtime.timechain.Start(context.Background()); err != nil {
        log.Error("Failed to start TimeChain: %v", err)
    }
}

    hostImports.AddModule(NewLogModule())
    hostImports.AddModule(NewBalanceModule())
    hostImports.AddModule(NewStateAccessModule())
    hostImports.AddModule(NewContractModule(runtime))

    if eventManager != nil {
        hostImports.AddModule(NewEventModule(eventManager))
    }

    linker, err := hostImports.createLinker(runtime)
    if err != nil {
        panic(err)
    }

    runtime.linker = linker

    return runtime
}

// Events returns the event manager
func (r *WasmRuntime) Events() *events.Manager {
    return r.eventManager
}

// WithDefaults creates a new call context with default values
func (r *WasmRuntime) WithDefaults(callInfo CallInfo) CallContext {
    if r.eventManager != nil {
        callInfo.events = make([]events.Event, 0)
    }

    return CallContext{
        r:               r,
        defaultCallInfo: callInfo,
    }
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

// CallContract executes a contract with given call info
func (r *WasmRuntime) CallContract(ctx context.Context, callInfo *CallInfo) (*chain.Result, error) {
    contractID, err := callInfo.State.GetAccountContract(ctx, callInfo.Contract)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }

    // Add event context
    if r.eventManager != nil {
        ctx = r.eventManager.WithContext(ctx)
    }

    // Get verified time if TimeChain enabled
    if r.timechain != nil {
        verifiedTime, proof, err := r.timechain.Roughtime.GetVerifiedTimeWithProof()
        if err != nil {
            return &chain.Result{
                Success: false,
                Error:   []byte(fmt.Sprintf("time verification failed: %v", err)),
            }, nil
        }
        
        callInfo.Timestamp = uint64(verifiedTime.Unix())
        
        // Add to time sequence
        entry := &timechain.SequenceEntry{
            VerifiedTime:  verifiedTime,
            TimeProof:     proof,
            SequenceNum:   r.timechain.GetNextSequenceNum(),
            Transactions:  []timechain.Transaction{}, // Add relevant transactions
        }
        
        if err := r.timechain.Sequence.AddToSequence(entry); err != nil {
            return &chain.Result{
                Success: false,
                Error:   []byte(fmt.Sprintf("time sequence failed: %v", err)),
            }, nil
        }
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

    if len(output.Outputs) > 0 {
        result.Outputs = append(result.Outputs, output.Outputs...)
    }

    // Process events
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

// getInstance creates a new contract instance
func (r *WasmRuntime) getInstance(contractModule *wasmtime.Module) (*ContractInstance, error) {
    store := wasmtime.NewStore(r.engine)
    store.SetEpochDeadline(1)
    inst, err := r.linker.Instantiate(store, contractModule)
    if err != nil {
        return nil, err
    }
    return &ContractInstance{inst: inst, store: store}, nil
}

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

// OnBlockCommitted handles new block commitments
func (r *WasmRuntime) OnBlockCommitted(height uint64, timestamp uint64) error {
    if r.eventManager != nil {
        return r.eventManager.OnBlockCommitted(height, timestamp)
    }
    return nil
}

// OnBlockRolledBack handles block rollbacks
func (r *WasmRuntime) OnBlockRolledBack(height uint64) error {
    if r.eventManager != nil {
        return r.eventManager.OnBlockRolledBack(height)
    }
    return nil
}

// Shutdown cleans up runtime resources
func (r *WasmRuntime) Shutdown(ctx context.Context) error {
    if r.eventManager != nil {
        if err := r.eventManager.Shutdown(ctx); err != nil {
            return err
        }
    }

    // Clean up TimeChain
    if r.timechain != nil {
        if err := r.timechain.Shutdown(ctx); err != nil {
            return err
        }
    }
    
    // Clean up other resources
    r.contractCache.Flush()
    r.callerInfo = make(map[uintptr]*CallInfo)
    
    return nil
}

// GetTimeSequence returns the current time sequence
func (r *WasmRuntime) GetTimeSequence() ([]*timechain.SequenceEntry, error) {
    if r.timechain == nil {
        return nil, errors.New("timechain not enabled")
    }
    return r.timechain.Sequence.GetSequence(), nil
}

// VerifyTimeSequence verifies sequence integrity
func (r *WasmRuntime) VerifyTimeSequence(sequence []*timechain.SequenceEntry) error {
    if r.timechain == nil {
        return errors.New("timechain not enabled")
    }
    return r.timechain.VerifySequence(sequence)
}