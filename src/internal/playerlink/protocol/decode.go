package protocol

import (
	"bytes"
	"encoding/binary"
)

type bodyReader struct {
	body     []byte
	position int
}

func (r *bodyReader) take(count int) ([]byte, error) {
	if count < 0 || r.position+count > len(r.body) {
		return nil, protocolError("body ends before the expected value")
	}
	chunk := r.body[r.position : r.position+count]
	r.position += count
	return chunk, nil
}

func (r *bodyReader) uint32() (uint32, error) {
	chunk, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(chunk), nil
}

func Decode(signature string, body []byte) (Values, error) {
	if len(body) > MaxBodySize {
		return nil, protocolError("body is larger than %d bytes", MaxBodySize)
	}
	fields, err := parseSignature(signature)
	if err != nil {
		return nil, err
	}
	reader := &bodyReader{body: body}
	values := make(Values, 0, len(fields))
	for _, field := range fields {
		value, err := reader.field(field)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if reader.position != len(body) {
		return nil, protocolError("%d unexpected bytes after the last value", len(body)-reader.position)
	}
	return values, nil
}

func (r *bodyReader) field(field fieldType) (any, error) {
	if !field.isArray() {
		return r.scalar(field.basic)
	}
	count, err := r.uint32()
	if err != nil {
		return nil, err
	}
	if field.arrayElement == typeByte {
		chunk, err := r.take(int(count))
		return bytes.Clone(chunk), err
	}
	if count > MaxArrayItems {
		return nil, protocolError("array has more than %d items", MaxArrayItems)
	}
	if field.structFields != "" {
		return r.structRows(field.structFields, int(count))
	}
	items := make([]any, 0, count)
	for range count {
		item, err := r.scalar(field.arrayElement)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *bodyReader) structRows(members string, count int) ([][]any, error) {
	rows := make([][]any, 0, count)
	for range count {
		row := make([]any, 0, len(members))
		for index := range len(members) {
			value, err := r.scalar(members[index])
			if err != nil {
				return nil, err
			}
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *bodyReader) scalar(code byte) (any, error) {
	switch code {
	case typeByte, typeBoolean:
		chunk, err := r.take(1)
		if err != nil {
			return nil, err
		}
		return chunk[0], nil
	case typeInt16Bits, typeUint16:
		chunk, err := r.take(2)
		if err != nil {
			return nil, err
		}
		return binary.LittleEndian.Uint16(chunk), nil
	case typeInt32:
		value, err := r.uint32()
		return int32(value), err
	case typeUint32:
		return r.uint32()
	case typeInt64, typeUint64, typeDouble:
		chunk, err := r.take(opaqueSize)
		if err != nil {
			return nil, err
		}
		return [opaqueSize]byte(chunk), nil
	case typeString:
		return r.string()
	}
	return nil, protocolError("unknown scalar type %q", code)
}

func (r *bodyReader) string() ([]byte, error) {
	size, err := r.uint32()
	if err != nil {
		return nil, err
	}
	chunk, err := r.take(int(size))
	if err != nil {
		return nil, err
	}
	if size == 0 || chunk[size-1] != 0 || bytes.IndexByte(chunk[:size-1], 0) >= 0 {
		return nil, protocolError("string is not terminated by exactly one NUL byte")
	}
	return bytes.Clone(chunk[:size-1]), nil
}
