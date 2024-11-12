// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build debug

package runtime

import (
	"fmt"
	"os"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
)

const logCost = 1000

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

type LogEntry struct {
	Level   LogLevel
	Message RawBytes
}

func NewLogModule() *ImportModule {
	return &ImportModule{
		Name: "log",
		HostFunctions: map[string]HostFunction{
			"write": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						// Write log to stderr with contract address context
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] %s\n",
							callInfo.Contract.String(),
							string(input),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write log: %v", err)),
							}, nil
						}

						// Return success with the logged message in outputs
						return &chain.Result{
							Success: true,
							Outputs: [][]byte{input},
						}, nil
					},
				),
			},
			"write_with_level": {
				FuelCost: logCost,
				Function: FunctionNoOutput[LogEntry](
					func(callInfo *CallInfo, entry LogEntry) (*chain.Result, error) {
						// Write log with level to stderr
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] [%s] %s\n",
							callInfo.Contract.String(),
							entry.Level.String(),
							string(entry.Message),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write log: %v", err)),
							}, nil
						}

						// Serialize the full log entry for outputs
						logData, err := codec.Marshal(entry)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to serialize log entry: %v", err)),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{logData},
						}, nil
					},
				),
			},
			"debug": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] [DEBUG] %s\n",
							callInfo.Contract.String(),
							string(input),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write debug log: %v", err)),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{input},
						}, nil
					},
				),
			},
			"info": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] [INFO] %s\n",
							callInfo.Contract.String(),
							string(input),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write info log: %v", err)),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{input},
						}, nil
					},
				),
			},
			"warn": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] [WARN] %s\n",
							callInfo.Contract.String(),
							string(input),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write warn log: %v", err)),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{input},
						}, nil
					},
				),
			},
			"error": {
				FuelCost: logCost,
				Function: FunctionNoOutput[RawBytes](
					func(callInfo *CallInfo, input RawBytes) (*chain.Result, error) {
						_, err := fmt.Fprintf(os.Stderr, "[Contract %s] [ERROR] %s\n",
							callInfo.Contract.String(),
							string(input),
						)
						if err != nil {
							return &chain.Result{
								Success: false,
								Error:   []byte(fmt.Sprintf("failed to write error log: %v", err)),
							}, nil
						}

						return &chain.Result{
							Success: true,
							Outputs: [][]byte{input},
						}, nil
					},
				),
			},
		},
	}
}

// Helper functions for log handling
func validateLogMessage(message []byte) error {
	if len(message) == 0 {
		return fmt.Errorf("empty log message")
	}
	if len(message) > 1024 { // 1KB limit for log messages
		return fmt.Errorf("log message exceeds size limit")
	}
	return nil
}

func createLogResult(message []byte, level LogLevel) *chain.Result {
	entry := LogEntry{
		Level:   level,
		Message: message,
	}
	
	logData, err := codec.Marshal(entry)
	if err != nil {
		return &chain.Result{
			Success: false,
			Error:   []byte(fmt.Sprintf("failed to serialize log entry: %v", err)),
		}
	}

	return &chain.Result{
		Success: true,
		Outputs: [][]byte{logData},
	}
}
