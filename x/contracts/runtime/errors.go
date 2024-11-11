// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"errors"
	"fmt"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/bytecodealliance/wasmtime-go/v25"
)

var (
	// Common error types
	ErrOutOfFuel          = errors.New("out of fuel")
	ErrOutOfMemory        = errors.New("out of memory")
	ErrFunctionNotFound   = errors.New("function not found")
	ErrInvalidParameters  = errors.New("invalid parameters")
	ErrExecutionFailed    = errors.New("execution failed")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidAddress     = errors.New("invalid address")
	ErrSerializationFailed = errors.New("serialization failed")
)

// convertToTrap converts an error to a wasmtime Trap
func convertToTrap(err error) *wasmtime.Trap {
	if err == nil {
		return nil
	}
	var t *wasmtime.Trap
	switch {
	case errors.As(err, &t):
		return t
	case errors.Is(err, ErrOutOfFuel):
		return wasmtime.NewTrap("out of fuel")
	case errors.Is(err, ErrOutOfMemory):
		return wasmtime.NewTrap("out of memory")
	default:
		return wasmtime.NewTrap(err.Error())
	}
}

// convertToResult converts an error to a chain.Result
func convertToResult(err error) *chain.Result {
	if err == nil {
		return &chain.Result{
			Success: true,
			Outputs: [][]byte{},
		}
	}

	return &chain.Result{
		Success: false,
		Error:   []byte(err.Error()),
	}
}

// wrapResultError creates a chain.Result with an error message
func wrapResultError(format string, args ...interface{}) *chain.Result {
	return &chain.Result{
		Success: false,
		Error:   []byte(fmt.Sprintf(format, args...)),
	}
}

// successResult creates a successful chain.Result with optional outputs
func successResult(outputs ...[]byte) *chain.Result {
	return &chain.Result{
		Success: true,
		Outputs: outputs,
	}
}

// IsExecutionError checks if an error is a runtime execution error
func IsExecutionError(err error) bool {
	if err == nil {
		return false
	}
	var trap *wasmtime.Trap
	if errors.As(err, &trap) {
		return true
	}
	return errors.Is(err, ErrExecutionFailed)
}

// IsOutOfFuel checks if an error is due to running out of fuel
func IsOutOfFuel(err error) bool {
	if err == nil {
		return false
	}
	var trap *wasmtime.Trap
	if errors.As(err, &trap) {
		return errors.Is(err, ErrOutOfFuel)
	}
	return false
}

// IsOutOfMemory checks if an error is due to running out of memory
func IsOutOfMemory(err error) bool {
	if err == nil {
		return false
	}
	var trap *wasmtime.Trap
	if errors.As(err, &trap) {
		return errors.Is(err, ErrOutOfMemory)
	}
	return false
}

// ExtractTrapError gets the underlying error from a wasmtime.Trap
func ExtractTrapError(trap *wasmtime.Trap) error {
	if trap == nil {
		return nil
	}
	switch *trap.Code() {
	case wasmtime.OutOfFuel:
		return ErrOutOfFuel
	case wasmtime.UnreachableCodeReached:
		return ErrExecutionFailed
	default:
		return errors.New(trap.Message())
	}
}

// ResultFromError creates a chain.Result from any error type
func ResultFromError(err error) *chain.Result {
	if err == nil {
		return &chain.Result{
			Success: true,
			Outputs: [][]byte{},
		}
	}

	var trap *wasmtime.Trap
	if errors.As(err, &trap) {
		return &chain.Result{
			Success: false,
			Error:   []byte(ExtractTrapError(trap).Error()),
		}
	}

	return &chain.Result{
		Success: false,
		Error:   []byte(err.Error()),
	}
}

// IsSuccessResult checks if a chain.Result represents success
func IsSuccessResult(result *chain.Result) bool {
	return result != nil && result.Success
}

// GetResultError extracts error message from a failed chain.Result
func GetResultError(result *chain.Result) string {
	if result == nil || result.Success {
		return ""
	}
	return string(result.Error)
}

// MergeResults combines multiple chain.Results into one
func MergeResults(results ...*chain.Result) *chain.Result {
	if len(results) == 0 {
		return successResult()
	}

	// If any result failed, return the first failure
	for _, r := range results {
		if !r.Success {
			return r
		}
	}

	// Combine outputs from all successful results
	var allOutputs [][]byte
	for _, r := range results {
		allOutputs = append(allOutputs, r.Outputs...)
	}

	return &chain.Result{
		Success: true,
		Outputs: allOutputs,
	}
}
