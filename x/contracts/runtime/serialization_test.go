// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
    "bytes"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
)

func TestSerializationRawBytes(t *testing.T) {
    require := require.New(t)
    testBytes := RawBytes([]byte{0, 1, 2, 3})
    serializedBytes, err := Serialize(testBytes)
    require.NoError(err)
    require.Equal(([]byte)(testBytes), serializedBytes)

    b := new(bytes.Buffer)
    require.NoError(testBytes.customSerialize(b))
    require.Equal(([]byte)(testBytes), b.Bytes())

    deserialized, err := RawBytes{}.customDeserialize(b.Bytes())
    require.NoError(err)
    require.Equal(testBytes, *deserialized)

    deserialized, err = Deserialize[RawBytes](b.Bytes())
    require.NoError(err)
    require.Equal(testBytes, *deserialized)
}

func TestSerializationResult(t *testing.T) {
    require := require.New(t)
    testResult := Ok[byte, byte](1)

    serializedBytes, err := Serialize(testResult)
    require.NoError(err)
    require.Equal([]byte{resultOkPrefix, 1}, serializedBytes)

    b := new(bytes.Buffer)
    require.NoError(testResult.customSerialize(b))
    require.Equal([]byte{resultOkPrefix, 1}, b.Bytes())

    deserialized, err := Result[byte, byte]{}.customDeserialize(b.Bytes())
    require.NoError(err)
    require.Equal(testResult, *deserialized)

    deserialized, err = Deserialize[Result[byte, byte]](b.Bytes())
    require.NoError(err)
    require.Equal(testResult, *deserialized)
}

func TestSerializationOption(t *testing.T) {
    require := require.New(t)
    testOption := Some[byte](1)

    serializedBytes, err := Serialize(testOption)
    require.NoError(err)
    require.Equal([]byte{optionSomePrefix, 1}, serializedBytes)

    b := new(bytes.Buffer)
    require.NoError(testOption.customSerialize(b))
    require.Equal([]byte{optionSomePrefix, 1}, b.Bytes())

    deserialized, err := Option[byte]{}.customDeserialize(b.Bytes())
    require.NoError(err)
    require.Equal(testOption, *deserialized)

    deserialized, err = Deserialize[Option[byte]](b.Bytes())
    require.NoError(err)
    require.Equal(testOption, *deserialized)
}

func TestSerializationChainResult(t *testing.T) {
    require := require.New(t)

    // Test successful result
    successResult := &chain.Result{
        Success: true,
        Outputs: [][]byte{{1, 2, 3}},
    }

    serialized, err := SerializeResult(successResult)
    require.NoError(err)

    deserialized, err := DeserializeResult(serialized)
    require.NoError(err)
    require.True(deserialized.Success)
    require.Equal(successResult.Outputs, deserialized.Outputs)

    // Test error result
    errorResult := &chain.Result{
        Success: false,
        Error:   []byte("test error"),
    }

    serialized, err = SerializeResult(errorResult)
    require.NoError(err)

    deserialized, err = DeserializeResult(serialized)
    require.NoError(err)
    require.False(deserialized.Success)
    require.Equal(errorResult.Error, deserialized.Error)
}

func TestSerializationComplexTypes(t *testing.T) {
    require := require.New(t)

    // Test nested Result with Option
    nested := Ok[Option[byte], byte](Some[byte](1))
    
    serialized, err := Serialize(nested)
    require.NoError(err)

    deserialized, err := Deserialize[Result[Option[byte], byte]](serialized)
    require.NoError(err)
    require.Equal(nested, *deserialized)

    // Test Option with Result
    optNested := Some[Result[byte, byte]](Ok[byte, byte](1))
    
    serialized, err = Serialize(optNested)
    require.NoError(err)

    deserialized2, err := Deserialize[Option[Result[byte, byte]]](serialized)
    require.NoError(err)
    require.Equal(optNested, *deserialized2)
}

func TestSerializationErrors(t *testing.T) {
    require := require.New(t)

    // Test empty data
    _, err := RawBytes{}.customDeserialize([]byte{})
    require.Error(err)

    // Test invalid Result prefix
    _, err = Result[byte, byte]{}.customDeserialize([]byte{3, 1}) // 3 is invalid prefix
    require.Error(err)

    // Test invalid Option prefix
    _, err = Option[byte]{}.customDeserialize([]byte{3, 1}) // 3 is invalid prefix
    require.Error(err)

    // Test invalid chain.Result
    _, err = DeserializeResult([]byte{})
    require.Error(err)
}

func TestSerializationNilHandling(t *testing.T) {
    require := require.New(t)

    // Test nil RawBytes
    var nilBytes RawBytes
    serialized, err := Serialize(nilBytes)
    require.NoError(err)
    require.Empty(serialized)

    // Test nil chain.Result
    var nilResult *chain.Result
    _, err = SerializeResult(nilResult)
    require.Error(err)
}

// Helper functions for testing serialization
func verifySerializationRoundTrip[T any](t *testing.T, value T) {
    require := require.New(t)
    
    serialized, err := Serialize(value)
    require.NoError(err)
    
    deserialized, err := Deserialize[T](serialized)
    require.NoError(err)
    require.Equal(value, *deserialized)
}

func verifyResultRoundTrip(t *testing.T, result *chain.Result) {
    require := require.New(t)
    
    serialized, err := SerializeResult(result)
    require.NoError(err)
    
    deserialized, err := DeserializeResult(serialized)
    require.NoError(err)
    require.Equal(result.Success, deserialized.Success)
    require.Equal(result.Error, deserialized.Error)
    require.Equal(result.Outputs, deserialized.Outputs)
}