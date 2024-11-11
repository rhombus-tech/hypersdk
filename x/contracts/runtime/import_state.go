// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"context"
	"errors"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/hypersdk/codec"
)

const (
	getCost    = 10000
	putCost    = 10000
	deleteCost = 10000
	putManyCost = 10000
)

type keyValueInput struct {
	Key   []byte
	Value []byte
}

func NewStateAccessModule() *ImportModule {
	return &ImportModule{
		Name: "state",
		HostFunctions: map[string]HostFunction{
			"get": {
				FuelCost: getCost,
				Function: Function[[]byte, RawBytes](
					func(callInfo *CallInfo, input []byte) (*chain.Result, error) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()

						val, err := callInfo.State.GetContractState(callInfo.Contract).GetValue(ctx, input)
						if err != nil {
							if errors.Is(err, database.ErrNotFound) {
								return &chain.Result{
									Success: true,
									Outputs: [][]byte{}, // Empty output for not found
								}, nil
							}
							return &chain.Result{
								Success: false,
								Error:   []byte(err.Error()),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{val},
						}, nil
					},
				),
			},
			"put": {
				FuelCost: putManyCost,
				Function: Function[[]keyValueInput, bool](
					func(callInfo *CallInfo, input []keyValueInput) (*chain.Result, error) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()

						contractState := callInfo.State.GetContractState(callInfo.Contract)
						for _, entry := range input {
							if len(entry.Value) == 0 {
								if err := contractState.Remove(ctx, entry.Key); err != nil {
									return &chain.Result{
										Success: false,
										Error:   []byte(err.Error()),
									}, nil
								}
							} else {
								if err := contractState.Insert(ctx, entry.Key, entry.Value); err != nil {
									return &chain.Result{
										Success: false,
										Error:   []byte(err.Error()),
									}, nil
								}
							}
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(true)},
						}, nil
					},
				),
			},
			"delete": {
				FuelCost: deleteCost,
				Function: Function[[]byte, bool](
					func(callInfo *CallInfo, key []byte) (*chain.Result, error) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()

						err := callInfo.State.GetContractState(callInfo.Contract).Remove(ctx, key)
						if err != nil {
							if errors.Is(err, database.ErrNotFound) {
								return &chain.Result{
									Success: true,
									Outputs: [][]byte{codec.MustMarshal(true)},
								}, nil
							}
							return &chain.Result{
								Success: false,
								Error:   []byte(err.Error()),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(true)},
						}, nil
					},
				),
			},
			"put_many": {
				FuelCost: putManyCost,
				Function: Function[[]keyValueInput, bool](
					func(callInfo *CallInfo, entries []keyValueInput) (*chain.Result, error) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()

						contractState := callInfo.State.GetContractState(callInfo.Contract)
						
						// Pre-validate all entries
						for _, entry := range entries {
							if len(entry.Key) == 0 {
								return &chain.Result{
									Success: false,
									Error:   []byte("empty key not allowed"),
								}, nil
							}
						}

						// Process all entries
						for _, entry := range entries {
							if len(entry.Value) == 0 {
								if err := contractState.Remove(ctx, entry.Key); err != nil && !errors.Is(err, database.ErrNotFound) {
									return &chain.Result{
										Success: false,
										Error:   []byte(err.Error()),
									}, nil
								}
							} else {
								if err := contractState.Insert(ctx, entry.Key, entry.Value); err != nil {
									return &chain.Result{
										Success: false,
										Error:   []byte(err.Error()),
									}, nil
								}
							}
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{codec.MustMarshal(true)},
						}, nil
					},
				),
			},
		},
	}
}

// Helper function to handle state operation errors
func handleStateError(err error) *chain.Result {
	if err == nil {
		return &chain.Result{
			Success: true,
			Outputs: [][]byte{},
		}
	}

	if errors.Is(err, database.ErrNotFound) {
		return &chain.Result{
			Success: true,
			Outputs: [][]byte{}, // Empty output for not found
		}
	}

	return &chain.Result{
		Success: false,
		Error:   []byte(err.Error()),
	}
}

// Helper function to create success result with boolean output
func boolResult(value bool) *chain.Result {
	return &chain.Result{
		Success: true,
		Outputs: [][]byte{codec.MustMarshal(value)},
	}
}

// Helper function to validate key-value pairs
func validateKeyValue(kv keyValueInput) error {
	if len(kv.Key) == 0 {
		return errors.New("empty key not allowed")
	}
	return nil
}