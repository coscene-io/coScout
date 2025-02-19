package mcap_ros2

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

const (
	typeBool    = "bool"
	typeByte    = "byte"
	typeChar    = "char"
	typeFloat32 = "float32"
	typeFloat64 = "float64"
	typeInt8    = "int8"
	typeUint8   = "uint8"
	typeInt16   = "int16"
	typeUint16  = "uint16"
	typeInt32   = "int32"
	typeUint32  = "uint32"
	typeInt64   = "int64"
	typeUint64  = "uint64"
	typeString  = "string"
)

// MessageSpecification represents a ROS2 message definition.
type MessageSpecification struct {
	PkgName   string
	MsgName   string
	Fields    []Field
	Constants []Constant
}

// Field represents a field in a ROS2 message.
type Field struct {
	Type         Type
	Name         string
	DefaultValue interface{}
}

// Type represents a ROS2 message field type.
type Type struct {
	PkgName      string
	Type         string
	IsArray      bool
	ArraySize    *int
	IsUpperBound bool
}

// Constant represents a constant in a ROS2 message.
type Constant struct {
	Type  string
	Name  string
	Value string
}

// DecoderFunction is a function that decodes ROS2 message data.
type DecoderFunction func(data []byte) (map[string]interface{}, error)

// GenerateDynamic converts a ROS2 concatenated message definition into a map of message parsers.
func GenerateDynamic(schemaName string, schemaText string) (map[string]DecoderFunction, error) {
	timeDefinition := MessageSpecification{
		PkgName: "builtin_interfaces",
		MsgName: "Time",
		Fields: []Field{
			{Type: Type{Type: "uint32"}, Name: "sec"},
			{Type: Type{Type: "uint32"}, Name: "nanosec"},
		},
	}
	msgDefs := map[string]MessageSpecification{
		"builtin_interfaces/Time":     timeDefinition,
		"builtin_interfaces/Duration": timeDefinition,
	}
	decoders := make(map[string]DecoderFunction)

	handleMsgDef := func(curSchemaName string, shortName string, msgDef MessageSpecification) {
		// Add the message definition to the dictionary
		msgDefs[curSchemaName] = msgDef
		msgDefs[shortName] = msgDef

		// Add the message decoder to the dictionary
		decoder := makeReadMessage(curSchemaName, msgDefs)
		decoders[curSchemaName] = decoder
		decoders[shortName] = decoder
	}

	if err := forEachMsgDef(schemaName, schemaText, handleMsgDef); err != nil {
		return nil, err
	}

	return decoders, nil
}

// makeReadMessage creates a decoder function for a specific message type.
func makeReadMessage(schemaName string, msgDefs map[string]MessageSpecification) DecoderFunction {
	return func(data []byte) (map[string]interface{}, error) {
		return ReadMessage(schemaName, msgDefs, data)
	}
}

// ReadMessage deserializes a ROS2 message from bytes.
func ReadMessage(schemaName string, msgDefs map[string]MessageSpecification, data []byte) (map[string]interface{}, error) {
	msgDef, ok := msgDefs[schemaName]
	if !ok {
		return nil, errors.Errorf("message definition not found for %q", schemaName)
	}

	reader, err := NewCdrReader(data)
	if err != nil {
		return nil, err
	}

	return readComplexType(msgDef, msgDefs, reader)
}

// forEachMsgDef processes each message definition in the schema text.
func forEachMsgDef(schemaName string, schemaText string, fn func(string, string, MessageSpecification)) error {
	curSchemaName := schemaName

	// Remove empty lines
	lines := strings.Split(schemaText, "\n")
	nonEmptyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}
	schemaText = strings.Join(nonEmptyLines, "\n")

	// Split schema text by separator lines containing at least 3 = characters
	parts := regexp.MustCompile(`(?m)^={3,}$`).Split(schemaText, -1)

	for _, curSchemaText := range parts {
		curSchemaText = strings.TrimSpace(curSchemaText)
		if curSchemaText == "" {
			continue
		}

		// Check for a "MSG: pkg_name/msg_name" line
		msgMatch := regexp.MustCompile(`(?m)^MSG:\s+(\S+)$`).FindStringSubmatch(curSchemaText)
		if len(msgMatch) > 0 {
			curSchemaName = msgMatch[1]
			// Remove this line from the message definition
			curSchemaText = regexp.MustCompile(`(?m)^MSG:\s+(\S+)$`).ReplaceAllString(curSchemaText, "")
		}

		// Parse the package and message names from the schema name
		parts := strings.Split(curSchemaName, "/")
		if len(parts) < 2 {
			return errors.Errorf("invalid schema name format: %s", curSchemaName)
		}
		pkgName := parts[0]
		msgName := parts[len(parts)-1]
		shortName := pkgName + "/" + msgName

		msgDef, err := parseMessageString(pkgName, msgName, curSchemaText)
		if err != nil {
			return errors.Errorf("failed to parse message string: %v", err)
		}

		fn(curSchemaName, shortName, msgDef)
	}

	return nil
}

// parseMessageString parses a message definition string into a MessageSpecification.
// todo: not quite matching python version.
func parseMessageString(pkgName string, msgName string, schemaText string) (MessageSpecification, error) {
	msgSpec := MessageSpecification{
		PkgName:   pkgName,
		MsgName:   msgName,
		Fields:    []Field{},
		Constants: []Constant{},
	}

	lines := strings.Split(schemaText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle comment lines
		if strings.HasPrefix(line, "#") {
			// Try to parse as constant, but ignore if it's just a comment
			constant, err := parseConstant(line)
			if err == nil {
				msgSpec.Constants = append(msgSpec.Constants, constant)
			}
			continue
		}

		// Handle fields
		field, err := parseField(line, pkgName)
		if err != nil {
			return msgSpec, errors.Errorf("failed to parse field '%s': %v", line, err)
		}
		msgSpec.Fields = append(msgSpec.Fields, field)
	}

	return msgSpec, nil
}

// parseConstant parses a constant definition line.
func parseConstant(line string) (Constant, error) {
	// Remove comment character and trim.
	line = strings.TrimSpace(strings.TrimPrefix(line, "#"))

	// A valid constant must have at least 3 parts: type name = value.
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[1] != "=" {
		return Constant{}, errors.Errorf("not a valid constant")
	}

	return Constant{
		Type:  parts[0],
		Name:  parts[2],
		Value: strings.Join(parts[3:], " "),
	}, nil
}

// parseField parses a field definition line.
func parseField(line string, currentPkg string) (Field, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return Field{}, errors.Errorf("invalid field format: %s", line)
	}

	// Parse type
	typeStr := parts[0]
	isArray := strings.HasSuffix(typeStr, "[]")
	if isArray {
		typeStr = strings.TrimSuffix(typeStr, "[]")
	}

	// Handle primitive types
	if isPrimitiveType(typeStr) {
		return Field{
			Type: Type{
				Type:    typeStr,
				IsArray: isArray,
			},
			Name: parts[1],
		}, nil
	}

	// Handle complex types
	// Split type into package and type name
	typeParts := strings.Split(typeStr, "/")
	if len(typeParts) == 1 {
		// If no package specified, use current package
		return Field{
			Type: Type{
				PkgName: currentPkg,
				Type:    typeStr,
				IsArray: isArray,
			},
			Name: parts[1],
		}, nil
	}

	// If package is specified, use it
	return Field{
		Type: Type{
			PkgName: typeParts[0],
			Type:    typeParts[1],
			IsArray: isArray,
		},
		Name: parts[1],
	}, nil
}

// isPrimitiveType checks if a type is a ROS2 primitive type.
func isPrimitiveType(typeName string) bool {
	switch typeName {
	case typeBool, typeByte, typeChar,
		typeFloat32, typeFloat64,
		typeInt8, typeUint8,
		typeInt16, typeUint16,
		typeInt32, typeUint32,
		typeInt64, typeUint64,
		typeString:
		return true
	default:
		return false
	}
}

// readComplexType reads a complex message type from the CDR reader.
func readComplexType(
	msgDef MessageSpecification,
	msgDefs map[string]MessageSpecification,
	reader *CdrReader,
) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Handle empty messages
	if len(msgDef.Fields) == 0 {
		// ROS2 adds a uint8 field for empty messages
		_, err := reader.Uint8()
		return result, err
	}

	for _, field := range msgDef.Fields {
		value, err := readField(field, msgDefs, reader)
		if err != nil {
			return nil, errors.Errorf("error reading field %s: %v", field.Name, err)
		}
		result[field.Name] = value
	}

	return result, nil
}

// readField reads a single field from the CDR reader.
func readField(
	field Field,
	msgDefs map[string]MessageSpecification,
	reader *CdrReader,
) (interface{}, error) {
	ftype := field.Type

	if isPrimitiveType(ftype.Type) {
		if ftype.IsArray {
			return readPrimitiveArray(ftype, reader)
		}
		return readPrimitiveValue(ftype.Type, reader)
	}

	// Complex type
	typeName := ftype.PkgName + "/" + ftype.Type
	// Remove leading slash if present
	typeName = strings.TrimPrefix(typeName, "/")
	nestedDef, ok := msgDefs[typeName]
	if !ok {
		return nil, errors.Errorf("message definition not found for %s", typeName)
	}

	if !ftype.IsArray {
		return readComplexType(nestedDef, msgDefs, reader)
	}

	var arrayLength int
	if ftype.ArraySize != nil {
		arrayLength = *ftype.ArraySize
	} else {
		// Read dynamic array length
		length, err := reader.Uint32()
		if err != nil {
			return nil, err
		}
		arrayLength = int(length)
	}

	array := make([]interface{}, arrayLength)
	for i := range arrayLength {
		value, err := readComplexType(nestedDef, msgDefs, reader)
		if err != nil {
			return nil, err
		}
		array[i] = value
	}
	return array, nil
}

// readPrimitiveArray reads an array of primitive values.
func readPrimitiveArray(ftype Type, reader *CdrReader) (interface{}, error) {
	var arrayLength int
	if ftype.ArraySize != nil {
		arrayLength = *ftype.ArraySize
	} else {
		// Read dynamic array length
		length, err := reader.Uint32()
		if err != nil {
			return nil, err
		}
		arrayLength = int(length)
	}

	switch ftype.Type {
	case typeBool:
		return reader.BooleanArray(arrayLength)
	case typeByte, typeUint8:
		return reader.Uint8Array(arrayLength)
	case typeChar, typeInt8:
		return reader.Int8Array(arrayLength)
	case typeInt16:
		return reader.Int16Array(arrayLength)
	case typeUint16:
		return reader.Uint16Array(arrayLength)
	case typeInt32:
		return reader.Int32Array(arrayLength)
	case typeUint32:
		return reader.Uint32Array(arrayLength)
	case typeInt64:
		return reader.Int64Array(arrayLength)
	case typeUint64:
		return reader.Uint64Array(arrayLength)
	case typeFloat32:
		return reader.Float32Array(arrayLength)
	case typeFloat64:
		return reader.Float64Array(arrayLength)
	case typeString:
		return reader.StringArray(arrayLength)
	default:
		return nil, errors.Errorf("unsupported array type: %s", ftype.Type)
	}
}

// readPrimitiveValue reads a single primitive value.
func readPrimitiveValue(typeName string, reader *CdrReader) (interface{}, error) {
	switch typeName {
	case typeBool:
		return reader.Boolean()
	case typeByte, typeUint8:
		return reader.Uint8()
	case typeChar, typeInt8:
		return reader.Int8()
	case typeInt16:
		return reader.Int16()
	case typeUint16:
		return reader.Uint16()
	case typeInt32:
		return reader.Int32()
	case typeUint32:
		return reader.Uint32()
	case typeInt64:
		return reader.Int64()
	case typeUint64:
		return reader.Uint64()
	case typeFloat32:
		return reader.Float32()
	case typeFloat64:
		return reader.Float64()
	case typeString:
		return reader.String()
	default:
		return nil, errors.Errorf("unsupported primitive type: %s", typeName)
	}
}
