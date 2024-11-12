// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "context"
    "testing"

    "github.com/near/borsh-go"
    "github.com/stretchr/testify/require"
    
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
)

func TestImportStatePutGet(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    // Test putting value
    result, err := contract.Call("put", int64(10))
    require.NoError(err)
    require.True(result.Success)

    // Test getting value
    result, err = contract.Call("get")
    require.NoError(err)
    require.True(result.Success)
    valueBytes, err := Serialize(int64(10))
    require.NoError(err)
    expected := Some[RawBytes](valueBytes)
    actual := into[Option[RawBytes]](result.Outputs[0])
    require.Equal(expected, actual)
}

func TestImportStateRemove(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    valueBytes, err := borsh.Serialize(int64(10))
    require.NoError(err)

    // Put value
    result, err := contract.Call("put", int64(10))
    require.NoError(err)
    require.True(result.Success)

    // Delete value and verify return
    result, err = contract.Call("delete")
    require.NoError(err)
    require.True(result.Success)
    expected := Some[RawBytes](valueBytes)
    actual := into[Option[RawBytes]](result.Outputs[0])
    require.Equal(expected, actual)

    // Verify value is gone
    result, err = contract.Call("get")
    require.NoError(err)
    require.True(result.Success)
    require.Equal(None[RawBytes](), into[Option[RawBytes]](result.Outputs[0]))
}

func TestImportStateDeleteMissingKey(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    // Try to delete non-existent key
    result, err := contract.Call("delete")
    require.NoError(err)
    require.True(result.Success)
    require.Equal(None[RawBytes](), into[Option[RawBytes]](result.Outputs[0]))
}

func TestImportStateGetMissingKey(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    // Try to get non-existent key
    result, err := contract.Call("get")
    require.NoError(err)
    require.True(result.Success)
    require.Equal(None[RawBytes](), into[Option[RawBytes]](result.Outputs[0]))
}

func TestImportStatePutMany(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    // Put multiple values
    keyValues := []struct {
        key   string
        value int64
    }{
        {"key1", 10},
        {"key2", 20},
        {"key3", 30},
    }

    for _, kv := range keyValues {
        result, err := contract.Call("put", kv.key, kv.value)
        require.NoError(err)
        require.True(result.Success)
    }

    // Verify all values
    for _, kv := range keyValues {
        result, err := contract.Call("get", kv.key)
        require.NoError(err)
        require.True(result.Success)
        valueBytes, err := Serialize(kv.value)
        require.NoError(err)
        expected := Some[RawBytes](valueBytes)
        actual := into[Option[RawBytes]](result.Outputs[0])
        require.Equal(expected, actual)
    }
}

func TestImportStateInvalidOperations(t *testing.T) {
    require := require.New(t)
    ctx := context.Background()

    rt := newTestRuntime(ctx)
    contract, err := rt.newTestContract("state_access")
    require.NoError(err)

    // Test putting nil value
    result, err := contract.Call("put", "key", nil)
    require.NoError(err)
    require.False(result.Success)

    // Test putting empty key
    result, err = contract.Call("put", "", int64(10))
    require.NoError(err)
    require.False(result.Success)
}

// Helper functions
func verifyStateValue[T any](t *testing.T, result *chain.Result, expected T) {
    require := require.New(t)
    require.True(result.Success)
    valueBytes, err := Serialize(expected)
    require.NoError(err)
    actual := into[Option[RawBytes]](result.Outputs[0])
    require.Equal(Some[RawBytes](valueBytes), actual)
}

func verifyNoValue(t *testing.T, result *chain.Result) {
    require := require.New(t)
    require.True(result.Success)
    require.Equal(None[RawBytes](), into[Option[RawBytes]](result.Outputs[0]))
}

func verifyStateError(t *testing.T, result *chain.Result, expectedError string) {
    require := require.New(t)
    require.False(result.Success)
    require.Contains(string(result.Error), expectedError)
}