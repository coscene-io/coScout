// Copyright 2025 coScene
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
	"encoding/json"
	"math"

	"github.com/pkg/errors"
)

const (
	// maxDecodedArrayBytes caps the estimated backing storage of any decoded
	// array. It is deliberately independent of the encoded payload size because
	// []string and []interface{} amplify small encoded elements into two-word Go
	// values.
	maxDecodedArrayBytes = 64 << 20

	// Interfaces and strings both occupy two machine words on the supported
	// 64-bit targets. Keeping the conservative 16-byte estimate on 32-bit
	// targets only makes the decoder limit stricter.
	resultInterfaceSlotBytes = 16
	resultStringHeaderBytes  = 16
)

// CdrReader parses values from CDR data.
type CdrReader struct {
	data         []byte
	offset       int
	littleEndian bool
}

// NewCdrReader creates a new CdrReader instance.
func NewCdrReader(data []byte) (*CdrReader, error) {
	if len(data) < 4 {
		return nil, errors.Errorf("invalid CDR data size %d, must contain at least a 4-byte header", len(data))
	}

	return &CdrReader{
		data:         data,
		offset:       4,
		littleEndian: data[1]&1 == 1,
	}, nil
}

// DecodedBytes returns the number of bytes that have been decoded.
func (r *CdrReader) DecodedBytes() int {
	return r.offset
}

// ByteLength returns the number of bytes in the CDR data.
func (r *CdrReader) ByteLength() int {
	return len(r.data)
}

// RemainingBytes returns the number of unread bytes in the CDR payload.
func (r *CdrReader) RemainingBytes() int {
	if r.offset >= len(r.data) {
		return 0
	}
	return len(r.data) - r.offset
}

// Boolean reads an 8-bit value and interprets it as a boolean.
func (r *CdrReader) Boolean() (bool, error) {
	val, err := r.Uint8()
	return val != 0, err
}

// Int8 reads a signed 8-bit integer.
func (r *CdrReader) Int8() (int8, error) {
	val, err := r.read(1)
	return int8(val[0]), err
}

// Uint8 reads an unsigned 8-bit integer.
func (r *CdrReader) Uint8() (uint8, error) {
	val, err := r.read(1)
	return val[0], err
}

// uint16ToInt16 converts a uint16 to a int16.
func uint16ToInt16(u uint16) int16 {
	//nolint: gosec // this is a safe conversion
	return int16(u&math.MaxInt16) - int16(u>>15)*math.MaxInt16
}

// Int16 reads a signed 16-bit integer.
func (r *CdrReader) Int16() (int16, error) {
	val, err := r.read(2)
	return uint16ToInt16(r.byteOrder().Uint16(val)), err
}

// Uint16 reads an unsigned 16-bit integer.
func (r *CdrReader) Uint16() (uint16, error) {
	val, err := r.read(2)
	return r.byteOrder().Uint16(val), err
}

// uint32ToInt32 converts a uint32 to a int32.
func uint32ToInt32(u uint32) int32 {
	//nolint: gosec // this is a safe conversion
	return int32(u&math.MaxInt32) - int32(u>>31)*math.MaxInt32
}

// Int32 reads a signed 32-bit integer.
func (r *CdrReader) Int32() (int32, error) {
	val, err := r.read(4)
	return uint32ToInt32(r.byteOrder().Uint32(val)), err
}

// Uint32 reads an unsigned 32-bit integer.
func (r *CdrReader) Uint32() (uint32, error) {
	val, err := r.read(4)
	return r.byteOrder().Uint32(val), err
}

// uint64ToInt64 converts a uint64 to a int64.
func uint64ToInt64(u uint64) int64 {
	//nolint: gosec // this is a safe conversion
	return int64(u&math.MaxInt64) - int64(u>>63)*math.MaxInt64
}

// Int64 reads a signed 64-bit integer.
func (r *CdrReader) Int64() (int64, error) {
	val, err := r.read(8)
	return uint64ToInt64(r.byteOrder().Uint64(val)), err
}

// Uint64 reads an unsigned 64-bit integer.
func (r *CdrReader) Uint64() (uint64, error) {
	val, err := r.read(8)
	return r.byteOrder().Uint64(val), err
}

// Float32 reads a 32-bit floating point number.
func (r *CdrReader) Float32() (JSONFloat32, error) {
	val, err := r.read(4)
	return JSONFloat32(math.Float32frombits(r.byteOrder().Uint32(val))), err
}

// Float64 reads a 64-bit floating point number.
func (r *CdrReader) Float64() (JSONFloat64, error) {
	val, err := r.read(8)
	return JSONFloat64(math.Float64frombits(r.byteOrder().Uint64(val))), err
}

// String reads a string prefixed with its 32-bit length.
func (r *CdrReader) String() (string, error) {
	length, err := r.Uint32()
	if err != nil {
		return "", err
	}

	if uint64(length) > uint64(maxInt()) {
		return "", errors.Errorf("string declared length %d cannot fit in int", length)
	}

	data, err := r.Uint8Array(int(length))
	if err != nil {
		return "", err
	}
	if length <= 1 {
		return "", nil
	}

	return string(data[:len(data)-1]), nil
}

// StringRaw reads a string of the given length.
func (r *CdrReader) StringRaw(length int) (string, error) {
	data, err := r.Uint8Array(length)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SequenceLength reads a 32-bit unsigned integer.
func (r *CdrReader) SequenceLength() (uint32, error) {
	return r.Uint32()
}

// byteOrder returns the appropriate byte order based on endianness.
func (r *CdrReader) byteOrder() binary.ByteOrder {
	if r.littleEndian {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// align aligns the offset to the given size.
func (r *CdrReader) align(size int) {
	alignment := (r.offset - 4) % size
	if alignment > 0 {
		r.offset += size - alignment
	}
}

// read reads bytes from the current offset and advances the offset.
func (r *CdrReader) read(size int) ([]byte, error) {
	if size < 0 {
		return nil, errors.Errorf("attempt to read a negative size %d", size)
	}

	// Align before reading if size > 1
	if size > 1 {
		r.align(size)
	}

	if r.offset > len(r.data) || size > len(r.data)-r.offset {
		return nil, errors.Errorf("attempt to read past end of data (offset: %d, size: %d, data length: %d)", r.offset, size, len(r.data))
	}

	data := r.data[r.offset : r.offset+size]
	r.offset += size
	return data, nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

// validateArrayLength validates a declared array length before it is converted
// to int or used by make/slicing.
func validateArrayLength(length uint64, remainingBytes, minEncodedBytes, resultElementBytes int) error {
	if remainingBytes < 0 || minEncodedBytes <= 0 || resultElementBytes <= 0 {
		return errors.Errorf(
			"invalid array validation parameters (remaining=%d, minimum encoded element=%d, result element=%d)",
			remainingBytes,
			minEncodedBytes,
			resultElementBytes,
		)
	}

	remainingLimit := uint64(remainingBytes / minEncodedBytes)
	if length > remainingLimit {
		return errors.Errorf(
			"array declared length %d exceeds remaining-data limit %d (remaining=%d bytes, minimum encoded element=%d bytes)",
			length,
			remainingLimit,
			remainingBytes,
			minEncodedBytes,
		)
	}

	allocationLimit := uint64(maxDecodedArrayBytes / resultElementBytes)
	if length > allocationLimit {
		return errors.Errorf(
			"array declared length %d exceeds decoder allocation limit %d elements (%d bytes, result element=%d bytes)",
			length,
			allocationLimit,
			maxDecodedArrayBytes,
			resultElementBytes,
		)
	}

	if length > uint64(maxInt()) {
		return errors.Errorf("array declared length %d cannot fit in int", length)
	}

	return nil
}

func (r *CdrReader) validateArrayLength(length, minEncodedBytes, resultElementBytes int) error {
	if length < 0 {
		return errors.Errorf("array declared a negative length %d", length)
	}
	return validateArrayLength(
		uint64(length),
		r.RemainingBytes(),
		minEncodedBytes,
		resultElementBytes,
	)
}

// BooleanArray reads an array of booleans.
func (r *CdrReader) BooleanArray(length int) ([]bool, error) {
	if err := r.validateArrayLength(length, 1, 1); err != nil {
		return nil, err
	}
	result := make([]bool, length)
	for i := range length {
		val, err := r.Boolean()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Uint8Array reads an array of uint8 values.
func (r *CdrReader) Uint8Array(length int) ([]uint8, error) {
	if err := r.validateArrayLength(length, 1, 1); err != nil {
		return nil, err
	}
	data := r.data[r.offset : r.offset+length]
	r.offset += length
	return data, nil
}

// Int8Array reads an array of int8 values.
func (r *CdrReader) Int8Array(length int) ([]int8, error) {
	if err := r.validateArrayLength(length, 1, 1); err != nil {
		return nil, err
	}
	result := make([]int8, length)
	for i := range length {
		val, err := r.Int8()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Int16Array reads an array of int16 values.
func (r *CdrReader) Int16Array(length int) ([]int16, error) {
	if err := r.validateArrayLength(length, 2, 2); err != nil {
		return nil, err
	}
	result := make([]int16, length)
	for i := range length {
		val, err := r.Int16()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Uint16Array reads an array of uint16 values.
func (r *CdrReader) Uint16Array(length int) ([]uint16, error) {
	if err := r.validateArrayLength(length, 2, 2); err != nil {
		return nil, err
	}
	result := make([]uint16, length)
	for i := range length {
		val, err := r.Uint16()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Int32Array reads an array of int32 values.
func (r *CdrReader) Int32Array(length int) ([]int32, error) {
	if err := r.validateArrayLength(length, 4, 4); err != nil {
		return nil, err
	}
	result := make([]int32, length)
	for i := range length {
		val, err := r.Int32()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Uint32Array reads an array of uint32 values.
func (r *CdrReader) Uint32Array(length int) ([]uint32, error) {
	if err := r.validateArrayLength(length, 4, 4); err != nil {
		return nil, err
	}
	result := make([]uint32, length)
	for i := range length {
		val, err := r.Uint32()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Int64Array reads an array of int64 values.
func (r *CdrReader) Int64Array(length int) ([]int64, error) {
	if err := r.validateArrayLength(length, 8, 8); err != nil {
		return nil, err
	}
	result := make([]int64, length)
	for i := range length {
		val, err := r.Int64()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Uint64Array reads an array of uint64 values.
func (r *CdrReader) Uint64Array(length int) ([]uint64, error) {
	if err := r.validateArrayLength(length, 8, 8); err != nil {
		return nil, err
	}
	result := make([]uint64, length)
	for i := range length {
		val, err := r.Uint64()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Float32Array reads an array of float32 values.
func (r *CdrReader) Float32Array(length int) ([]JSONFloat32, error) {
	if err := r.validateArrayLength(length, 4, 4); err != nil {
		return nil, err
	}
	result := make([]JSONFloat32, length)
	for i := range length {
		val, err := r.Float32()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// Float64Array reads an array of float64 values.
func (r *CdrReader) Float64Array(length int) ([]JSONFloat64, error) {
	if err := r.validateArrayLength(length, 8, 8); err != nil {
		return nil, err
	}
	result := make([]JSONFloat64, length)
	for i := range length {
		val, err := r.Float64()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// StringArray reads an array of strings.
func (r *CdrReader) StringArray(length int) ([]string, error) {
	if err := r.validateArrayLength(length, 4, resultStringHeaderBytes); err != nil {
		return nil, err
	}
	result := make([]string, length)
	for i := range length {
		val, err := r.String()
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// JSONFloat64 is a float64 that marshals to a string, handling inf and nan.
// TODO: inf would be represented as +Inf in JSON, and unmarshalling of that
// will result in a string at the moment, but that should not hurt anything at the moment.
type JSONFloat64 float64

func (j JSONFloat64) MarshalJSON() ([]byte, error) {
	v := float64(j)
	switch {
	case math.IsInf(v, 1):
		return json.Marshal("+Inf")
	case math.IsInf(v, -1):
		return json.Marshal("-Inf")
	case math.IsNaN(v):
		return json.Marshal("NaN")
	default:
		return json.Marshal(v) // marshal result as standard float64
	}
}

// JSONFloat32 is a float32 that marshals to a string, handling inf and nan.
// TODO: inf would be represented as +Inf in JSON, and unmarshalling of that
// will result in a string at the moment, but that should not hurt anything at the moment.
type JSONFloat32 float32

func (j JSONFloat32) MarshalJSON() ([]byte, error) {
	v := float32(j)
	switch {
	case math.IsInf(float64(v), 1):
		return json.Marshal("+Inf")
	case math.IsInf(float64(v), -1):
		return json.Marshal("-Inf")
	case math.IsNaN(float64(v)):
		return json.Marshal("NaN")
	default:
		return json.Marshal(v) // marshal result as standard float32
	}
}
