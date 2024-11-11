// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"errors"

	"github.com/bytecodealliance/wasmtime-go/v25"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
)

type ContractCallErrorCode byte

const (
	callContractCost  = 10000
	setCallResultCost = 10000
	remainingFuelCost = 10000
	deployCost        = 10000
)

const (
	ExecutionFailure ContractCallErrorCode = iota
	CallPanicked
	OutOfFuel
	InsufficientBalance
)

type callContractInput struct {
	Contract     codec.Address
	FunctionName string
	Params       []byte
	Fuel         uint64
	Value        uint64
}

type deployContractInput struct {
	ContractID          ContractID
	AccountCreationData []byte
}

func ExtractContractCallErrorCode(err error) (ContractCallErrorCode, bool) {
	var trap *wasmtime.Trap
	if errors.As(err, &trap) {
		switch *trap.Code() {
		case wasmtime.UnreachableCodeReached:
			return CallPanicked, true
		case wasmtime.OutOfFuel:
			return OutOfFuel, true
		default:
			return ExecutionFailure, true
		}
	}
	return 0, false
}

func NewContractModule(r *WasmRuntime) *ImportModule {
	return &ImportModule{
		Name: "contract",
		HostFunctions: map[string]HostFunction{
			"call_contract": {
				FuelCost: callContractCost,
				Function: Function[callContractInput, Result[RawBytes, ContractCallErrorCode]](
					func(callInfo *CallInfo, input callContractInput) (*chain.Result, error) {
						newInfo := *callInfo

						if err := callInfo.ConsumeFuel(input.Fuel); err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(OutOfFuel.String()),
							}, nil
						}

						newInfo.Actor = callInfo.Contract
						newInfo.Contract = input.Contract
						newInfo.FunctionName = input.FunctionName
						newInfo.Params = input.Params
						newInfo.Fuel = input.Fuel
						newInfo.Value = input.Value

						result, err := r.CallContract(
							context.Background(),
							&newInfo)
						
						if err != nil {
							if code, ok := ExtractContractCallErrorCode(err); ok {
								return &chain.Result{
									Success: false,
									Error:   []byte(code.String()),
								}, nil
							}
							return &chain.Result{
								Success: false,
								Error:   []byte(ExecutionFailure.String()),
							}, err
						}

						// Return any remaining fuel to the calling contract
						callInfo.AddFuel(newInfo.RemainingFuel())

						return result, nil
					}),
			},
			"set_call_result": {
				FuelCost: setCallResultCost,
				Function: Function[RawBytes, bool](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						// needs to clone because this points into the current store's linear memory 
						// which may be gone when this is read
						callInfo.inst.result = slices.Clone(input)
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(true)},
						}, nil
					}),
			},
			"remaining_fuel": {
				FuelCost: remainingFuelCost,
				Function: FunctionNoInput[uint64](
					func(callInfo *CallInfo) (*chain.Result, error) {
						remaining := callInfo.RemainingFuel()
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(remaining)},
						}, nil
					}),
			},
			"deploy": {
				FuelCost: deployCost,
				Function: Function[deployContractInput, codec.Address](
					func(callInfo *CallInfo, input deployContractInput) (*chain.Result, error) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						
						address, err := callInfo.State.NewAccountWithContract(
							ctx,
							input.ContractID,
							input.AccountCreationData)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(err.Error()),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(address)},
						}, nil
					}),
			},
		},
	}
}

// Helper methods for error codes
func (c ContractCallErrorCode) String() string {
	switch c {
	case ExecutionFailure:
		return "execution failure"
	case CallPanicked:
		return "call panicked"
	case OutOfFuel:
		return "out of fuel"
	case InsufficientBalance:
		return "insufficient balance"
	default:
		return "unknown error"
	}
}

// Helper for creating error results
func contractErrorResult(code ContractCallErrorCode) *chain.Result {
	return &chain.Result{
		Success: false,
		Error:   []byte(code.String()),
	}
}

// Helper for successful contract call results
func contractSuccessResult(output []byte) *chain.Result {
	return &chain.Result{
		Success: true,
		Outputs: [][]byte{output},
	}
}

// Helper for checking if a contract exists
func validateContract(ctx context.Context, state StateManager, contract codec.Address) (*chain.Result, error) {
	_, err := state.GetAccountContract(ctx, contract)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte("contract not found"),
		}, nil
	}
	return nil, nil
}

// Helper for validating contract calls
func validateContractCall(input callContractInput) error {
	if input.Contract == codec.EmptyAddress {
		return errors.New("invalid contract address")
	}
	if input.FunctionName == "" {
		return errors.New("function name required")
	}
	return nil
}

// Helper for validating deployment
func validateDeployment(input deployContractInput) error {
	if len(input.ContractID) == 0 {
		return errors.New("contract ID required")
	}
	return nil
}

// Helper for handling contract call results
func handleContractCallResult(result *chain.Result, remainingFuel uint64) *chain.Result {
	if !result.Success {
		return result
	}

	// Add remaining fuel information to successful results
	fuelBytes, err := codec.Marshal(remainingFuel)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte("failed to serialize remaining fuel"),
		}
	}

	result.Outputs = append(result.Outputs, fuelBytes)
	return result
}