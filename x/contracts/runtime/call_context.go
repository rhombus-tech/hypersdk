// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
)

type CallContext struct {
	r               *WasmRuntime
	defaultCallInfo CallInfo
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
                return nil, fmt.Errorf("%w %s", ErrCannotOverwrite, callInfoTypeInfo.Field(i).Name)
            }
            resultField.Set(defaultField)
        }
    }
    return &newCallInfo, nil
}

func (c CallContext) CallContract(ctx context.Context, info *CallInfo) (*chain.Result, error) {
	newInfo, err := c.createCallInfo(info)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(err.Error()),
		}, nil
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

// New: Add method to combine multiple changes
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

// New: Add helper method to validate call info
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

// New: Add method to get default values
func (c CallContext) GetDefaults() CallInfo {
	return c.defaultCallInfo
}

// New: Add method to create a new context with merged defaults
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

// New: Add method to clone context
func (c CallContext) Clone() CallContext {
	return CallContext{
		r:               c.r,
		defaultCallInfo: c.defaultCallInfo,
	}
}