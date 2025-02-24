package mcap_ros2

import (
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/pkg/errors"
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

	if length <= 1 {
		r.offset += int(length)
		return "", nil
	}

	str, err := r.StringRaw(int(length - 1))
	if err != nil {
		return "", err
	}
	r.offset++ // Skip null terminator
	return str, nil
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
	if r.offset+size > len(r.data) {
		return nil, errors.Errorf("attempt to read past end of data (offset: %d, size: %d, data length: %d)", r.offset, size, len(r.data))
	}

	// Align before reading if size > 1
	if size > 1 {
		r.align(size)
	}

	data := r.data[r.offset : r.offset+size]
	r.offset += size
	return data, nil
}

// BooleanArray reads an array of booleans.
func (r *CdrReader) BooleanArray(length int) ([]bool, error) {
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
	data := r.data[r.offset : r.offset+length]
	r.offset += length
	return data, nil
}

// Int8Array reads an array of int8 values.
func (r *CdrReader) Int8Array(length int) ([]int8, error) {
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
