// Copyright 2026 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcap_ros2

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dynamicArrayData(length uint32, payload ...byte) []byte {
	data := make([]byte, 8, 8+len(payload))
	data[1] = 1 // Little-endian CDR.
	binary.LittleEndian.PutUint32(data[4:], length)
	return append(data, payload...)
}

func TestReadPrimitiveArrayRejectsLengthExceedingRemainingData(t *testing.T) {
	t.Parallel()

	reader, err := NewCdrReader(dynamicArrayData(math.MaxUint32))
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = readPrimitiveArray(Type{Type: typeUint8, IsArray: true}, reader)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declared length")
	assert.Contains(t, err.Error(), "remaining")
}

func TestReadComplexArrayRejectsLengthExceedingRemainingData(t *testing.T) {
	t.Parallel()

	reader, err := NewCdrReader(dynamicArrayData(math.MaxUint32))
	require.NoError(t, err)
	field := Field{
		Type: Type{
			PkgName: "test",
			Type:    "Empty",
			IsArray: true,
		},
		Name: "items",
	}
	msgDefs := map[string]MessageSpecification{
		"test/Empty": {PkgName: "test", MsgName: "Empty"},
	}

	require.NotPanics(t, func() {
		_, err = readField(field, msgDefs, reader)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declared length")
	assert.Contains(t, err.Error(), "remaining")
}

func TestReadComplexArrayRejectsHardAllocationLimit(t *testing.T) {
	t.Parallel()

	length := maxDecodedArrayBytes/resultComplexElementBytes + 1
	reader, err := NewCdrReader(dynamicArrayData(uint32(length), make([]byte, length)...))
	require.NoError(t, err)
	field := Field{
		Type: Type{
			PkgName: "test",
			Type:    "Empty",
			IsArray: true,
		},
		Name: "items",
	}
	msgDefs := map[string]MessageSpecification{
		"test/Empty": {PkgName: "test", MsgName: "Empty"},
	}

	_, err = readField(field, msgDefs, reader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoder allocation limit")
}

func TestValidateArrayLengthRejectsHardAllocationLimit(t *testing.T) {
	t.Parallel()

	length := uint64(maxDecodedArrayBytes/resultInterfaceSlotBytes + 1)

	_, err := validateArrayLength(length, math.MaxInt, 1, resultInterfaceSlotBytes)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoder allocation limit")
}

func TestReadPrimitiveArrayDecodesValidDynamicArray(t *testing.T) {
	t.Parallel()

	reader, err := NewCdrReader(dynamicArrayData(3, 1, 2, 3))
	require.NoError(t, err)

	value, err := readPrimitiveArray(Type{Type: typeUint8, IsArray: true}, reader)

	require.NoError(t, err)
	assert.Equal(t, []uint8{1, 2, 3}, value)
}

func TestReadComplexArrayDecodesValidDynamicArray(t *testing.T) {
	t.Parallel()

	reader, err := NewCdrReader(dynamicArrayData(2, 7, 8))
	require.NoError(t, err)
	field := Field{
		Type: Type{
			PkgName: "test",
			Type:    "Empty",
			IsArray: true,
		},
		Name: "items",
	}
	msgDefs := map[string]MessageSpecification{
		"test/Empty": {PkgName: "test", MsgName: "Empty"},
	}

	value, err := readField(field, msgDefs, reader)

	require.NoError(t, err)
	assert.Equal(t, []interface{}{
		map[string]interface{}{},
		map[string]interface{}{},
	}, value)
}

func TestReadPrimitiveArrayDecodesBoundedDynamicArray(t *testing.T) {
	t.Parallel()

	bound := 3
	reader, err := NewCdrReader(dynamicArrayData(2, 7, 8))
	require.NoError(t, err)

	value, err := readPrimitiveArray(Type{
		Type:         typeUint8,
		IsArray:      true,
		ArraySize:    &bound,
		IsUpperBound: true,
	}, reader)

	require.NoError(t, err)
	assert.Equal(t, []uint8{7, 8}, value)
}

func TestReadPrimitiveArrayRejectsSchemaBoundViolation(t *testing.T) {
	t.Parallel()

	bound := 3
	reader, err := NewCdrReader(dynamicArrayData(4, 1, 2, 3, 4))
	require.NoError(t, err)

	_, err = readPrimitiveArray(Type{
		Type:         typeUint8,
		IsArray:      true,
		ArraySize:    &bound,
		IsUpperBound: true,
	}, reader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema upper bound")
}

func TestReadPrimitiveFixedArrayRejectsTruncatedDataWithoutPanic(t *testing.T) {
	t.Parallel()

	arraySize := 3
	reader, err := NewCdrReader([]byte{0, 1, 0, 0, 1})
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = readPrimitiveArray(Type{
			Type:      typeUint8,
			IsArray:   true,
			ArraySize: &arraySize,
		}, reader)
	})
	require.Error(t, err)
}

func TestReadMessageRejectsTruncatedSequenceLengthWithoutPanic(t *testing.T) {
	t.Parallel()

	msgDefs := map[string]MessageSpecification{
		"test/Message": {
			PkgName: "test",
			MsgName: "Message",
			Fields: []Field{{
				Type: Type{Type: typeUint8, IsArray: true},
				Name: "values",
			}},
		},
	}
	data := []byte{0, 1, 0, 0, 2, 0}

	var err error
	require.NotPanics(t, func() {
		_, err = ReadMessage("test/Message", msgDefs, data)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "past end of data")
}

func TestScalarReadersReturnErrorsForTruncatedData(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*CdrReader) error{
		"boolean": func(reader *CdrReader) error {
			_, err := reader.Boolean()
			return err
		},
		"int8": func(reader *CdrReader) error {
			_, err := reader.Int8()
			return err
		},
		"uint8": func(reader *CdrReader) error {
			_, err := reader.Uint8()
			return err
		},
		"int16": func(reader *CdrReader) error {
			_, err := reader.Int16()
			return err
		},
		"uint16": func(reader *CdrReader) error {
			_, err := reader.Uint16()
			return err
		},
		"int32": func(reader *CdrReader) error {
			_, err := reader.Int32()
			return err
		},
		"uint32": func(reader *CdrReader) error {
			_, err := reader.Uint32()
			return err
		},
		"int64": func(reader *CdrReader) error {
			_, err := reader.Int64()
			return err
		},
		"uint64": func(reader *CdrReader) error {
			_, err := reader.Uint64()
			return err
		},
		"float32": func(reader *CdrReader) error {
			_, err := reader.Float32()
			return err
		},
		"float64": func(reader *CdrReader) error {
			_, err := reader.Float64()
			return err
		},
	}

	for name, read := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reader, err := NewCdrReader([]byte{0, 1, 0, 0})
			require.NoError(t, err)

			require.NotPanics(t, func() {
				err = read(reader)
			})
			require.Error(t, err)
		})
	}
}

func TestAlignedScalarReadReturnsErrorWithoutPanic(t *testing.T) {
	t.Parallel()

	reader, err := NewCdrReader([]byte{0, 1, 0, 0, 1, 2})
	require.NoError(t, err)
	_, err = reader.Uint8()
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, err = reader.Uint32()
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offset: 8")
}
