// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !debug

package runtime

import (
	"github.com/ava-labs/hypersdk/chain"
)

const logCost = 1000

// NewLogModule returns a minimal logging implementation for production use
func NewLogModule() *ImportModule {
	return &ImportModule{
		Name: "log",
		HostFunctions: map[string]HostFunction{
			"write": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(*CallInfo, RawBytes) (*chain.Result, error) {
						// In release mode, we still charge for logging but don't output anything
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
			"write_with_level": {
				FuelCost: logCost,
				Function: FunctionNoOutput[LogEntry](
					func(*CallInfo, LogEntry) (*chain.Result, error) {
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
			"debug": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(*CallInfo, RawBytes) (*chain.Result, error) {
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
			"info": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(*CallInfo, RawBytes) (*chain.Result, error) {
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
			"warn": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(*CallInfo, RawBytes) (*chain.Result, error) {
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
			"error": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(*CallInfo, RawBytes) (*chain.Result, error) {
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{},
						}, nil
					},
				),
			},
		},
	}
}

// LogLevel type for compatibility with debug version
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// LogEntry struct for compatibility with debug version
type LogEntry struct {
	Level   LogLevel
	Message RawBytes
}

// Empty implementations of helper functions for compatibility
func validateLogMessage(message []byte) error {
	return nil
}

func createLogResult(message []byte, level LogLevel) *chain.Result {
	return &chain.Result{
		Success: true,
		Outputs: [][]byte{},
	}
}