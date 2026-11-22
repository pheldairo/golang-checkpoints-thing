package protocol

import (
	"bytes"
	"encoding/binary"
)

const (
	KindCall         = 1
	KindReply        = 2
	KindNotification = 4
)

const (
	innerMagic       = 0xa5a5
	littleEndianMark = 0x6c
	innerVersion     = 1
	innerHeaderSize  = 16
	defaultFlags     = 1
	priorityMask     = 3
	maxPriority      = 2
)

type Message struct {
	Kind      uint8
	Serial    uint16
	Signature string
	Body      []byte
	Flags     uint8
}

func NewMessage(kind uint8, serial uint16, signature string, values []any) (Message, error) {
	body, err := Encode(signature, values)
	if err != nil {
		return Message{}, err
	}
	return Message{Kind: kind, Serial: serial, Signature: signature, Body: body, Flags: defaultFlags}, nil
}

func DispatchWord(service, action uint16) uint32 {
	return uint32(service)<<16 | uint32(action)
}

func NewCall(service, action, serial uint16, signature string, args []any) (Message, error) {
	if serial == 0 {
		return Message{}, protocolError("calls need a non-zero serial to match their reply")
	}
	values := append([]any{DispatchWord(service, action)}, args...)
	return NewMessage(KindCall, serial, "u"+signature, values)
}

func (m Message) Values() (Values, error) {
	return Decode(m.Signature, m.Body)
}

func (m Message) header() ([]byte, error) {
	if _, err := parseSignature(m.Signature); err != nil {
		return nil, err
	}
	if m.Kind != KindCall && m.Kind != KindReply && m.Kind != KindNotification {
		return nil, protocolError("unsupported message kind %d", m.Kind)
	}
	if m.Flags&priorityMask > maxPriority {
		return nil, protocolError("unsupported message priority %d", m.Flags&priorityMask)
	}
	if len(m.Body) > MaxBodySize {
		return nil, protocolError("body is larger than %d bytes", MaxBodySize)
	}
	signature := append([]byte(m.Signature), 0)
	header := make([]byte, 0, innerHeaderSize+len(signature))
	header = binary.LittleEndian.AppendUint16(header, innerMagic)
	header = append(header, littleEndianMark, m.Kind, m.Flags, innerVersion)
	header = binary.LittleEndian.AppendUint16(header, m.Serial)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(m.Body)))
	header = binary.LittleEndian.AppendUint32(header, uint32(len(signature)))
	return append(header, signature...), nil
}

func messageFromParts(header, body []byte) (Message, error) {
	if len(header) < innerHeaderSize+1 {
		return Message{}, protocolError("message header is truncated")
	}
	magic := binary.LittleEndian.Uint16(header[0:2])
	if magic != innerMagic || header[2] != littleEndianMark || header[5] != innerVersion {
		return Message{}, protocolError("message header has unsupported constants")
	}
	bodySize := binary.LittleEndian.Uint32(header[8:12])
	signatureSize := binary.LittleEndian.Uint32(header[12:16])
	if int(signatureSize) != len(header)-innerHeaderSize || int(bodySize) != len(body) {
		return Message{}, protocolError("message lengths do not match the header")
	}
	rawSignature := header[innerHeaderSize:]
	if rawSignature[len(rawSignature)-1] != 0 || bytes.IndexByte(rawSignature[:len(rawSignature)-1], 0) >= 0 {
		return Message{}, protocolError("signature is not terminated by exactly one NUL byte")
	}
	for _, character := range rawSignature[:len(rawSignature)-1] {
		if character > 0x7f {
			return Message{}, protocolError("signature contains non-ASCII bytes")
		}
	}
	message := Message{
		Kind:      header[3],
		Flags:     header[4],
		Serial:    binary.LittleEndian.Uint16(header[6:8]),
		Signature: string(rawSignature[:len(rawSignature)-1]),
		Body:      bytes.Clone(body),
	}
	if _, err := message.header(); err != nil {
		return Message{}, err
	}
	if _, err := message.Values(); err != nil {
		return Message{}, err
	}
	return message, nil
}
