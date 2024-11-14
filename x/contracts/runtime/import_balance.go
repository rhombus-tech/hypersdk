// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
	"errors"

    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
)

const (
    sendBalanceCost = 10000
    getBalanceCost  = 10000
)

type transferBalanceInput struct {
    To     codec.Address
    Amount uint64
}

func NewBalanceModule() *ImportModule {
    return &ImportModule{
        Name: "balance",
        HostFunctions: map[string]HostFunction{
            "get": {
                FuelCost: getBalanceCost,
                Function: Function[codec.Address, uint64](
                    func(callInfo *CallInfo, address codec.Address) (*chain.Result, error) {
                        ctx, cancel := context.WithCancel(context.Background())
                        defer cancel()

                        balance, err := callInfo.State.GetBalance(ctx, address)
                        if err != nil {
                            return &chain.Result{
                                Success: false,
                                Error:   []byte(err.Error()),
                            }, nil
                        }

                        // Serialize the balance
                        balanceBytes, err := Serialize(balance)
                        if err != nil {
                            return &chain.Result{
                                Success: false,
                                Error:   []byte("failed to serialize balance"),
                            }, nil
                        }

                        return &chain.Result{
                            Success: true,
                            Outputs: [][]byte{balanceBytes},
                        }, nil
                    },
                ),
            },
            "send": {
                FuelCost: sendBalanceCost,
                Function: Function[transferBalanceInput, Result[Unit, ContractCallErrorCode]](
                    func(callInfo *CallInfo, input transferBalanceInput) (*chain.Result, error) {
                        ctx, cancel := context.WithCancel(context.Background())
                        defer cancel()

                        err := callInfo.State.TransferBalance(ctx, callInfo.Contract, input.To, input.Amount)
                        if err != nil {
                            if extractedError, ok := ExtractContractCallErrorCode(err); ok {
                                return &chain.Result{
                                    Success: false,
                                    Error:   []byte(extractedError.String()),
                                }, nil
                            }
                            return &chain.Result{
                                Success: false,
                                Error:   []byte("execution failure"),
                            }, nil
                        }

                        // Serialize the success result
                        successData, err := Serialize(Ok[Unit, ContractCallErrorCode](Unit{}))
                        if err != nil {
                            return &chain.Result{
                                Success: false,
                                Error:   []byte("failed to serialize result"),
                            }, nil
                        }

                        return &chain.Result{
                            Success: true,
                            Outputs: [][]byte{successData},
                        }, nil
                    },
                ),
            },
        },
    }
}

// Helper function to handle balance errors
func handleBalanceError(err error) *chain.Result {
    if extractedError, ok := ExtractContractCallErrorCode(err); ok {
        return &chain.Result{
            Success: false,
            Error:   []byte(extractedError.String()),
        }
    }
    return &chain.Result{
        Success: false,
        Error:   []byte(err.Error()),
    }
}

// Helper function to create success result with amount
func amountResult(amount uint64) (*chain.Result, error) {
	amountBytes, err := Serialize(amount)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte("failed to serialize amount"),
		}, nil
	}

	return &chain.Result{
		Success: true,
		Outputs: [][]byte{amountBytes},
	}, nil
}

// Helper function to validate transfer input
func validateTransferInput(input transferBalanceInput) error {
	if input.To == codec.EmptyAddress {
		return errors.New("invalid recipient address")
	}
	if input.Amount == 0 {
		return errors.New("transfer amount must be greater than zero")
	}
	return nil
}

// Additional helper functions for common balance operations

// CreateTransferResult creates a chain.Result for a successful transfer
func CreateTransferResult(amount uint64, from, to codec.Address) *chain.Result {
	transferData := struct {
		Amount uint64
		From   codec.Address
		To     codec.Address
	}{
		Amount: amount,
		From:   from,
		To:     to,
	}

	transferBytes, err := Serialize(transferData)
if err != nil {
    return &chain.Result{
        Success: false,
        Error:   []byte("failed to serialize transfer data"),
    }
}

	return &chain.Result{
		Success: true,
		Outputs: [][]byte{transferBytes},
	}
}

// CreateBalanceResult creates a chain.Result for a balance query
func CreateBalanceResult(balance uint64) (*chain.Result, error) {
	balanceBytes, err := Serialize(balance)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte("failed to serialize balance"),
		}, nil
	}

	return &chain.Result{
		Success: true,
		Outputs: [][]byte{balanceBytes},
	}, nil
}

// Fuel calculation helpers

// CalculateTransferFuel calculates the total fuel cost for a transfer
func CalculateTransferFuel(baseAmount uint64) uint64 {
	return sendBalanceCost + baseAmount
}

// CalculateBalanceCheckFuel calculates the total fuel cost for a balance check
func CalculateBalanceCheckFuel() uint64 {
	return getBalanceCost
}