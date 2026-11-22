package protocol

import (
	"bytes"
	"encoding/binary"
	"math"
)

func Encode(signature string, values []any) ([]byte, error) {
	fields, err := parseSignature(signature)
	if err != nil {
		return nil, err
	}
	if len(fields) != len(values) {
		return nil, protocolError("signature %q needs %d values, got %d", signature, len(fields), len(values))
	}
	var body []byte
	for index, field := range fields {
		body, err = appendField(body, field, values[index])
		if err != nil {
			return nil, err
		}
		if len(body) > MaxBodySize {
			return nil, protocolError("encoded body is larger than %d bytes", MaxBodySize)
		}
	}
	return body, nil
}

func appendField(body []byte, field fieldType, value any) ([]byte, error) {
	switch {
	case field.structFields != "":
		return appendStructArray(body, field.structFields, value)
	case field.arrayElement == typeByte:
		return appendByteArray(body, value)
	case field.arrayElement != 0:
		return appendBasicArray(body, field.arrayElement, value)
	default:
		return appendScalar(body, field.basic, value)
	}
}

func appendByteArray(body []byte, value any) ([]byte, error) {
	raw, ok := value.([]byte)
	if !ok {
		return nil, protocolError("byte array needs []byte, got %T", value)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(raw)))
	return append(body, raw...), nil
}

func appendBasicArray(body []byte, element byte, value any) ([]byte, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, protocolError("array of %q needs []any, got %T", element, value)
	}
	if len(items) > MaxArrayItems {
		return nil, protocolError("array has more than %d items", MaxArrayItems)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(items)))
	for _, item := range items {
		var err error
		if body, err = appendScalar(body, element, item); err != nil {
			return nil, err
		}
	}
	return body, nil
}

func appendStructArray(body []byte, members string, value any) ([]byte, error) {
	rows, ok := value.([][]any)
	if !ok {
		return nil, protocolError("struct array needs [][]any, got %T", value)
	}
	if len(rows) > MaxArrayItems {
		return nil, protocolError("array has more than %d items", MaxArrayItems)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(rows)))
	for _, row := range rows {
		if len(row) != len(members) {
			return nil, protocolError("struct row has %d fields, signature needs %d", len(row), len(members))
		}
		for index := range len(members) {
			var err error
			if body, err = appendScalar(body, members[index], row[index]); err != nil {
				return nil, err
			}
		}
	}
	return body, nil
}

func appendScalar(body []byte, code byte, value any) ([]byte, error) {
	switch code {
	case typeByte, typeBoolean:
		return appendUnsigned(body, value, 1)
	case typeInt16Bits, typeUint16:
		return appendUnsigned(body, value, 2)
	case typeUint32:
		return appendUnsigned(body, value, 4)
	case typeInt32:
		number, err := signed32Value(value)
		if err != nil {
			return nil, err
		}
		return binary.LittleEndian.AppendUint32(body, uint32(number)), nil
	case typeInt64, typeUint64, typeDouble:
		opaque, err := opaqueValue(value)
		if err != nil {
			return nil, err
		}
		return append(body, opaque...), nil
	case typeString:
		return appendString(body, value)
	}
	return nil, protocolError("unknown scalar type %q", code)
}

func appendUnsigned(body []byte, value any, width int) ([]byte, error) {
	switch width {
	case 1:
		number, err := unsignedValue(value, math.MaxUint8)
		if err != nil {
			return nil, err
		}
		return append(body, byte(number)), nil
	case 2:
		number, err := unsignedValue(value, math.MaxUint16)
		if err != nil {
			return nil, err
		}
		return binary.LittleEndian.AppendUint16(body, uint16(number)), nil
	default:
		number, err := unsignedValue(value, math.MaxUint32)
		if err != nil {
			return nil, err
		}
		return binary.LittleEndian.AppendUint32(body, uint32(number)), nil
	}
}

func unsignedValue(value any, maximum uint64) (uint64, error) {
	var number uint64
	switch typed := value.(type) {
	case uint8:
		number = uint64(typed)
	case uint16:
		number = uint64(typed)
	case uint32:
		number = uint64(typed)
	case int:
		if typed < 0 {
			return 0, protocolError("value %d must not be negative", typed)
		}
		number = uint64(typed)
	default:
		return 0, protocolError("expected an unsigned number, got %T", value)
	}
	if number > maximum {
		return 0, protocolError("value %d is larger than %d", number, maximum)
	}
	return number, nil
}

func signed32Value(value any) (int32, error) {
	switch typed := value.(type) {
	case int32:
		return typed, nil
	case int:
		if typed < math.MinInt32 || typed > math.MaxInt32 {
			return 0, protocolError("value %d does not fit in 32 bits", typed)
		}
		return int32(typed), nil
	}
	return 0, protocolError("expected a signed 32-bit number, got %T", value)
}

func opaqueValue(value any) ([]byte, error) {
	switch typed := value.(type) {
	case [opaqueSize]byte:
		return typed[:], nil
	case []byte:
		if len(typed) == opaqueSize {
			return typed, nil
		}
	}
	return nil, protocolError("opaque 64-bit fields need exactly %d bytes", opaqueSize)
}

func appendString(body []byte, value any) ([]byte, error) {
	var text []byte
	switch typed := value.(type) {
	case string:
		text = []byte(typed)
	case []byte:
		text = typed
	default:
		return nil, protocolError("string field needs string or []byte, got %T", value)
	}
	if bytes.IndexByte(text, 0) >= 0 {
		return nil, protocolError("strings must not contain NUL bytes")
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(text)+1))
	body = append(body, text...)
	return append(body, 0), nil
}
