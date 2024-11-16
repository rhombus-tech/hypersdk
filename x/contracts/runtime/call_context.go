// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime/timechain"
)

var (
    errCannotOverwrite = errors.New("trying to overwrite set field")
    callInfoTypeInfo   = reflect.TypeOf(CallInfo{})
)

type CallContext struct {
    r               *WasmRuntime
    defaultCallInfo CallInfo
    timechain       *timechain.TimeChain // Add TimeChain support
}

func (c CallContext) createCallInfo(callInfo *CallInfo) (*CallInfo, error) {
    newCallInfo := *callInfo
    resultInfo := reflect.ValueOf(&newCallInfo)
    defaults := reflect.ValueOf(c.defaultCallInfo)
    for i := 0; i < defaults.NumField(); i++ {
        defaultField := defaults.Field(i)
        if !defaultField.IsZero() {
            resultField := resultInfo.Elem().Field(i)
            if !resultField.IsZero() {
                return nil, fmt.Errorf("%w %s", errCannotOverwrite, callInfoTypeInfo.Field(i).Name)
            }
            resultField.Set(defaultField)
        }
    }
    return &newCallInfo, nil
}

// WithVerifiedTime adds Roughtime verification to the context
func (c CallContext) WithVerifiedTime() (CallContext, error) {
    if c.r.timechain == nil {
        return c, nil
    }

    verifiedTime, proof, err := c.r.timechain.Roughtime.GetVerifiedTimeWithProof()
    if err != nil {
        return c, fmt.Errorf("time verification failed: %w", err)
    }

    c.defaultCallInfo.Timestamp = uint64(verifiedTime.Unix())
    // Store time proof in chain
    if err := c.r.timechain.AddTimeProof(proof); err != nil {
        return c, fmt.Errorf("failed to add time proof: %w", err)
    }

    return c, nil
}

func (c CallContext) CallContract(ctx context.Context, info *CallInfo) (*chain.Result, error) {
    // Add time verification if enabled
    if c.r.timechain != nil {
        var err error
        c, err = c.WithVerifiedTime()
        if err != nil {
            return &chain.Result{
                Success: false,
                Error:   []byte(err.Error()),
            }, nil
        }
    }

    newInfo, err := c.createCallInfo(info)
    if err != nil {
        return &chain.Result{
            Success: false,
            Error:   []byte(err.Error()),
        }, nil
    }

    // Create sequence entry if TimeChain enabled
    if c.r.timechain != nil {
        entry := &timechain.SequenceEntry{
            VerifiedTime: time.Unix(int64(newInfo.Timestamp), 0),
            Transactions: []timechain.Transaction{}, // Add relevant transactions
            SequenceNum:  c.r.timechain.GetNextSequenceNum(),
        }
        
        if err := c.r.timechain.Sequence.AddToSequence(entry); err != nil {
            return &chain.Result{
                Success: false,
                Error:   []byte(fmt.Sprintf("time sequence failed: %v", err)),
            }, nil
        }
    }

    return c.r.CallContract(ctx, newInfo)
}

func (c CallContext) WithStateManager(manager StateManager) CallContext {
    c.defaultCallInfo.State = manager
    return c
}

func (c CallContext) WithActor(address codec.Address) CallContext {
    c.defaultCallInfo.Actor = address
    return c
}

func (c CallContext) WithFunction(s string) CallContext {
    c.defaultCallInfo.FunctionName = s
    return c
}

func (c CallContext) WithContract(address codec.Address) CallContext {
    c.defaultCallInfo.Contract = address
    return c
}

func (c CallContext) WithFuel(u uint64) CallContext {
    c.defaultCallInfo.Fuel = u
    return c
}

func (c CallContext) WithParams(bytes []byte) CallContext {
    c.defaultCallInfo.Params = bytes
    return c
}

func (c CallContext) WithHeight(height uint64) CallContext {
    c.defaultCallInfo.Height = height
    return c
}

func (c CallContext) WithActionID(actionID ids.ID) CallContext {
    c.defaultCallInfo.ActionID = actionID
    return c
}

func (c CallContext) WithTimestamp(timestamp uint64) CallContext {
    c.defaultCallInfo.Timestamp = timestamp
    return c
}

func (c CallContext) WithValue(value uint64) CallContext {
    c.defaultCallInfo.Value = value
    return c
}

// WithOptions combines multiple changes
func (c CallContext) WithOptions(opts map[string]interface{}) CallContext {
    for key, value := range opts {
        switch key {
        case "state":
            if manager, ok := value.(StateManager); ok {
                c = c.WithStateManager(manager)
            }
        case "actor":
            if addr, ok := value.(codec.Address); ok {
                c = c.WithActor(addr)
            }
        case "function":
            if s, ok := value.(string); ok {
                c = c.WithFunction(s)
            }
        case "contract":
            if addr, ok := value.(codec.Address); ok {
                c = c.WithContract(addr)
            }
        case "fuel":
            if u, ok := value.(uint64); ok {
                c = c.WithFuel(u)
            }
        case "params":
            if b, ok := value.([]byte); ok {
                c = c.WithParams(b)
            }
        case "height":
            if h, ok := value.(uint64); ok {
                c = c.WithHeight(h)
            }
        case "actionID":
            if id, ok := value.(ids.ID); ok {
                c = c.WithActionID(id)
            }
        case "timestamp":
            if ts, ok := value.(uint64); ok {
                c = c.WithTimestamp(ts)
            }
        case "value":
            if v, ok := value.(uint64); ok {
                c = c.WithValue(v)
            }
        }
    }
    return c
}

// Validate checks if call info is complete
func (c CallContext) Validate() error {
    if c.defaultCallInfo.State == nil {
        return errors.New("state manager is required")
    }
    if c.defaultCallInfo.Contract == codec.EmptyAddress {
        return errors.New("contract address is required")
    }
    if c.defaultCallInfo.FunctionName == "" {
        return errors.New("function name is required")
    }
    return nil
}

// GetDefaults returns default values
func (c CallContext) GetDefaults() CallInfo {
    return c.defaultCallInfo
}

// WithDefaults creates new context with merged defaults
func (c CallContext) WithDefaults(defaults CallInfo) CallContext {
    newContext := c
    resultInfo := reflect.ValueOf(&newContext.defaultCallInfo)
    defaultsValue := reflect.ValueOf(defaults)

    for i := 0; i < defaultsValue.NumField(); i++ {
        defaultField := defaultsValue.Field(i)
        if !defaultField.IsZero() {
            resultField := resultInfo.Elem().Field(i)
            if resultField.IsZero() {
                resultField.Set(defaultField)
            }
        }
    }

    return newContext
}

// Clone creates a copy of the context
func (c CallContext) Clone() CallContext {
    return CallContext{
        r:               c.r,
        defaultCallInfo: c.defaultCallInfo,
        timechain:       c.timechain,
    }
}

// GetTimeSequenceProof generates proof for a sequence entry
func (c CallContext) GetTimeSequenceProof(sequenceNum uint64) (*timechain.MerkleProof, error) {
    if c.r.timechain == nil {
        return nil, errors.New("timechain not enabled")
    }
    return c.r.timechain.MerkleTree.GenerateProof(int(sequenceNum))
}

// VerifyTimeSequence verifies sequence integrity
func (c CallContext) VerifyTimeSequence(start, end uint64) error {
    if c.r.timechain == nil {
        return errors.New("timechain not enabled")
    }
    sequence := c.r.timechain.Sequence.GetSequence()
    if start >= uint64(len(sequence)) || end >= uint64(len(sequence)) {
        return errors.New("invalid sequence range")
    }
    return c.r.timechain.VerifySequence(sequence[start:end+1])
}