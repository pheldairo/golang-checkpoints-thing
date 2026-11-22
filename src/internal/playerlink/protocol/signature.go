package protocol

import (
	"errors"
	"fmt"
	"strings"
)

var ErrProtocol = errors.New("player protocol error")

func protocolError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrProtocol, fmt.Sprintf(format, args...))
}

const (
	MaxBodySize        = 16 * 1024 * 1024
	MaxArrayItems      = 100_000
	maxSignatureLength = 1024
	opaqueSize         = 8
)

const (
	typeByte      = 'y'
	typeBoolean   = 'b'
	typeInt16Bits = 'n'
	typeUint16    = 'q'
	typeInt32     = 'i'
	typeUint32    = 'u'
	typeInt64     = 'x'
	typeUint64    = 't'
	typeDouble    = 'd'
	typeString    = 's'
	typeArray     = 'a'
	structOpen    = '('
	structClosed  = ')'
)

const basicTypes = "ybnqiuxtds"

type fieldType struct {
	basic        byte
	arrayElement byte
	structFields string
}

func (f fieldType) isArray() bool {
	return f.arrayElement != 0 || f.structFields != ""
}

func isBasicType(code byte) bool {
	return strings.IndexByte(basicTypes, code) >= 0
}

func parseSignature(signature string) ([]fieldType, error) {
	if len(signature) > maxSignatureLength {
		return nil, protocolError("signature is longer than %d characters", maxSignatureLength)
	}
	var fields []fieldType
	for position := 0; position < len(signature); {
		code := signature[position]
		position++
		if isBasicType(code) {
			fields = append(fields, fieldType{basic: code})
			continue
		}
		if code != typeArray {
			return nil, protocolError("unsupported type %q in signature %q", code, signature)
		}
		field, next, err := parseArray(signature, position)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
		position = next
	}
	return fields, nil
}

func parseArray(signature string, position int) (fieldType, int, error) {
	if position == len(signature) {
		return fieldType{}, 0, protocolError("array without element type in %q", signature)
	}
	element := signature[position]
	if element == structOpen {
		end := strings.IndexByte(signature[position:], structClosed)
		if end < 0 {
			return fieldType{}, 0, protocolError("unclosed struct array in %q", signature)
		}
		members := signature[position+1 : position+end]
		if members == "" || !allBasic(members) {
			return fieldType{}, 0, protocolError("struct arrays must hold a non-empty list of basic types")
		}
		return fieldType{structFields: members}, position + end + 1, nil
	}
	if !isBasicType(element) || element == typeString {
		return fieldType{}, 0, protocolError("unsupported array element %q", element)
	}
	return fieldType{arrayElement: element}, position + 1, nil
}

func allBasic(codes string) bool {
	for index := range len(codes) {
		if !isBasicType(codes[index]) {
			return false
		}
	}
	return true
}
