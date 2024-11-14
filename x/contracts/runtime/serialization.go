// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import (
	"bytes"
	"errors"
	"io"
	"reflect"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/near/borsh-go"
)

var (
	ErrDeserializationFailed = errors.New("deserialization failed")
)

type customSerialize interface {
	customSerialize(b io.Writer) error
}

type customDeserialize[T any] interface {
	customDeserialize([]byte) (*T, error)
}

func Deserialize[T any](data []byte) (*T, error) {
	result := new(T)
	var err error
	switch t := any(*result).(type) {
	case customDeserialize[T]:
		return t.customDeserialize(data)
	default:
		err = borsh.Deserialize(result, data)
	}
	return result, err
}

func Serialize[T any](value T) ([]byte, error) {
	b := &bytes.Buffer{}
	if err := bufferSerialize(value, b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func bufferSerialize[T any](value T, b io.Writer) error {
	if isNil(value) {
		return nil
	}
	switch t := any(value).(type) {
	case customSerialize:
		return t.customSerialize(b)
	default:
		return borsh.NewEncoder(b).Encode(value)
	}
}

func isNil[T any](t T) bool {
	v := reflect.ValueOf(t)
	kind := v.Kind()
	// Must be one of these types to be nillable
	return (kind == reflect.Ptr ||
		kind == reflect.Interface ||
		kind == reflect.Slice ||
		kind == reflect.Map ||
		kind == reflect.Chan ||
		kind == reflect.Func) &&
		v.IsNil()
}

// RawBytes handles raw byte data
type RawBytes []byte

func (r RawBytes) customSerialize(b io.Writer) error {
	_, err := b.Write(r)
	return err
}

func (RawBytes) customDeserialize(data []byte) (*RawBytes, error) {
	rawData := RawBytes(data)
	return &rawData, nil
}

// Result type constants
const (
	resultOkPrefix  = byte(1)
	resultErrPrefix = byte(0)
)

// Result handles operation results
type Result[T any, E any] struct {
	hasError bool
	value    T
	e        E
}

func Ok[T any, E any](val T) Result[T, E] {
	return Result[T, E]{value: val}
}

func Err[T any, E any](e E) Result[T, E] {
	return Result[T, E]{e: e, hasError: true}
}

func (r Result[T, E]) customSerialize(b io.Writer) error {
	var prefix []byte
	var val any
	if r.hasError {
		prefix = []byte{resultErrPrefix}
		val = r.e
	} else {
		prefix = []byte{resultOkPrefix}
		val = r.value
	}
	if _, err := b.Write(prefix); err != nil {
		return err
	}
	return bufferSerialize(val, b)
}

func (Result[T, E]) customDeserialize(data []byte) (*Result[T, E], error) {
	if len(data) < 1 {
		return nil, ErrDeserializationFailed
	}
	switch data[0] {
	case resultOkPrefix:
		{
			val, err := Deserialize[T](data[1:])
			if err != nil {
				return nil, ErrDeserializationFailed
			}
			return &Result[T, E]{value: *val}, nil
		}
	case resultErrPrefix:
		{
			val, err := Deserialize[E](data[1:])
			if err != nil {
				return nil, ErrDeserializationFailed
			}
			return &Result[T, E]{e: *val, hasError: true}, nil
		}
	default:
		return &Result[T, E]{}, ErrDeserializationFailed
	}
}

func (r Result[T, E]) Ok() (T, bool) {
	return r.value, !r.hasError
}

func (r Result[T, E]) Err() (E, bool) {
	return r.e, r.hasError
}

// Option type constants
const (
	optionSomePrefix = byte(1)
	optionNonePrefix = byte(0)
)

// Option handles optional values
type Option[T any] struct {
	isNone bool
	value  T
}

func Some[T any](val T) Option[T] {
	return Option[T]{value: val}
}

func None[T any]() Option[T] {
	return Option[T]{isNone: true}
}

func (o Option[T]) customSerialize(b io.Writer) error {
	if o.isNone {
		_, err := b.Write([]byte{optionNonePrefix})
		return err
	}
	if _, err := b.Write([]byte{optionSomePrefix}); err != nil {
		return err
	}
	return bufferSerialize(o.value, b)
}

func (Option[T]) customDeserialize(data []byte) (*Option[T], error) {
	if len(data) < 1 {
		return nil, ErrDeserializationFailed
	}
	switch data[0] {
	case optionSomePrefix:
		{
			val, err := Deserialize[T](data[1:])
			if err != nil {
				return nil, ErrDeserializationFailed
			}
			return &Option[T]{value: *val}, nil
		}
	case optionNonePrefix:
		{
			return &Option[T]{isNone: true}, nil
		}
	default:
		return &Option[T]{isNone: true}, ErrDeserializationFailed
	}
}

func (o Option[T]) Some() (T, bool) {
	return o.value, !o.isNone
}

func (o Option[T]) None() bool {
	return o.isNone
}

// Unit represents an empty value
type Unit struct{}

// Chain Result serialization helpers
func SerializeResult(result *chain.Result) ([]byte, error) {
    return borsh.Serialize(result)
}

func DeserializeResult(data []byte) (*chain.Result, error) {
    result := &chain.Result{}
    err := borsh.Deserialize(result, data)
    if err != nil {
        return nil, ErrDeserializationFailed
    }
    return result, nil
}

// Helper functions for common serialization tasks
func MustSerialize[T any](value T) []byte {
	data, err := Serialize(value)
	if err != nil {
		panic(err)
	}
	return data
}

func MustDeserialize[T any](data []byte) T {
	result, err := Deserialize[T](data)
	if err != nil {
		panic(err)
	}
	return *result
}

// Validation helpers
func ValidateSerializedData(data []byte, maxSize int) error {
	if len(data) == 0 {
		return errors.New("empty data")
	}
	if maxSize > 0 && len(data) > maxSize {
		return errors.New("data exceeds maximum size")
	}
	return nil
}

// Size calculation helper
func CalculateSerializedSize[T any](value T) (int, error) {
	data, err := Serialize(value)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}