// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
    "github.com/ava-labs/hypersdk/runtime/modules"
)

const (
    ModuleName = "event"

    // Operation fuel costs
    EmitEventCost        = 1000
    PingCost            = 5000
    PongCost            = 10000
    VerifySignatureCost = 20000
)

// Module provides event emission capabilities to WASM contracts
type Module struct {
    manager *Manager
}

// NewModule creates a new event module with the given manager
func NewModule(manager *Manager) modules.Module {
    return &Module{
        manager: manager,
    }
}

func (m *Module) Name() string {
    return ModuleName
}

// GetFunctions returns the module's functions that will be imported into WASM
func (m *Module) GetFunctions() map[string]modules.HostFunction {
    return map[string]modules.HostFunction{
        "emit": {
            FuelCost: EmitEventCost,
            Function: modules.FunctionNoOutput[[]byte](
                func(callInfo *modules.CallInfo, input []byte) error {
                    event := Event{
                        Contract:    callInfo.Contract,
                        BlockHeight: callInfo.Height,
                        Timestamp:   callInfo.Timestamp,
                        Data:       input,
                    }
                    // Validate event before emitting
                    if err := event.Validate(); err != nil {
                        return err
                    }
                    
                    m.manager.Emit(event)
                    return nil
                },
            ),
        },
        "emit_with_type": {
            FuelCost: EmitEventCost,
            Function: modules.Function[EmitParams, bool](
                func(callInfo *modules.CallInfo, params EmitParams) (bool, error) {
                    event := Event{
                        Contract:    callInfo.Contract,
                        EventType:   params.EventType,
                        Data:       params.Data,
                        BlockHeight: callInfo.Height,
                        Timestamp:   callInfo.Timestamp,
                    }
                    
                    // Validate event before emitting
                    if err := event.Validate(); err != nil {
                        return false, err
                    }
                    
                    m.manager.Emit(event)
                    return true, nil
                },
            ),
        },
        "ping": {
            FuelCost: PingCost,
            Function: modules.FunctionNoInput[bool](
                func(callInfo *modules.CallInfo) (bool, error) {
                    event := Event{
                        Contract:    callInfo.Contract,
                        EventType:   "Ping",
                        BlockHeight: callInfo.Height,
                        Timestamp:   callInfo.Timestamp,
                    }
                    
                    m.manager.Emit(event)
                    return true, nil
                },
            ),
        },
        "pong": {
            FuelCost: PongCost,
            Function: modules.Function[PongParams, bool](
                func(callInfo *modules.CallInfo, params PongParams) (bool, error) {
                    event := Event{
                        Contract:    callInfo.Contract,
                        EventType:   "Pong",
                        Data:       append(params.Signature, params.PublicKey...),
                        BlockHeight: callInfo.Height,
                        Timestamp:   callInfo.Timestamp,
                    }
                    
                    if err := event.Validate(); err != nil {
                        return false, err
                    }
                    
                    m.manager.Emit(event)
                    return true, nil
                },
            ),
        },
    }
}

// EmitParams represents the parameters for event emission
type EmitParams struct {
    EventType string
    Data      []byte
}

// PongParams represents the parameters for a pong response
type PongParams struct {
    PingTimestamp uint64
    Nonce         uint64
    Signature     []byte
    PublicKey     []byte
}

// Validate ensures event parameters are valid
func (e *EmitParams) Validate() error {
    if len(e.EventType) == 0 || len(e.EventType) > MaxEventTypeLength {
        return ErrInvalidEventType
    }
    if len(e.Data) > MaxEventDataSize {
        return ErrEventTooLarge
    }
    return nil
}

// Validate ensures pong parameters are valid
func (p *PongParams) Validate() error {
    // Add validation for signature, public key, etc.
    if len(p.Signature) == 0 {
        return ErrInvalidSignature
    }
    if len(p.PublicKey) == 0 {
        return ErrInvalidPublicKey
    }
    return nil
}