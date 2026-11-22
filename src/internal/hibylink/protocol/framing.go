package protocol

import "encoding/binary"

const (
	outerMagic       = 0x5aa5aa55
	outerVersion     = 0x100
	outerHeaderSize  = 24
	crcStart         = 6
	crcOffset        = 4
	MaxFrameSize     = 0x800
	SendFrameSize    = 0x7f0
	sendPayloadSize  = SendFrameSize - outerHeaderSize
	maxPendingGroups = 16
)

type Frame struct {
	Sequence  uint32
	Group     uint32
	Remaining uint32
	Payload   []byte
}

func (f Frame) Length() int {
	return outerHeaderSize + len(f.Payload)
}

func (f Frame) Bytes() ([]byte, error) {
	if f.Length() > MaxFrameSize || f.Remaining < uint32(f.Length()) {
		return nil, protocolError("frame lengths are invalid")
	}
	wire := make([]byte, 0, f.Length())
	wire = binary.LittleEndian.AppendUint32(wire, outerMagic)
	wire = binary.LittleEndian.AppendUint16(wire, 0)
	wire = binary.LittleEndian.AppendUint16(wire, outerVersion)
	wire = binary.LittleEndian.AppendUint32(wire, uint32(f.Length()))
	wire = binary.LittleEndian.AppendUint32(wire, f.Sequence)
	wire = binary.LittleEndian.AppendUint32(wire, f.Group)
	wire = binary.LittleEndian.AppendUint32(wire, f.Remaining)
	wire = append(wire, f.Payload...)
	binary.LittleEndian.PutUint16(wire[crcOffset:], CRC16(wire[crcStart:]))
	return wire, nil
}

type outerHeader struct {
	magic     uint32
	crc       uint16
	version   uint16
	size      uint32
	sequence  uint32
	group     uint32
	remaining uint32
}

func readOuterHeader(wire []byte) outerHeader {
	return outerHeader{
		magic:     binary.LittleEndian.Uint32(wire[0:4]),
		crc:       binary.LittleEndian.Uint16(wire[4:6]),
		version:   binary.LittleEndian.Uint16(wire[6:8]),
		size:      binary.LittleEndian.Uint32(wire[8:12]),
		sequence:  binary.LittleEndian.Uint32(wire[12:16]),
		group:     binary.LittleEndian.Uint32(wire[16:20]),
		remaining: binary.LittleEndian.Uint32(wire[20:24]),
	}
}

func (h outerHeader) hasValidConstants() bool {
	return h.magic == outerMagic && h.version == outerVersion &&
		h.size >= outerHeaderSize && h.size <= MaxFrameSize
}

func ParseFrame(wire []byte) (Frame, error) {
	if len(wire) < outerHeaderSize {
		return Frame{}, protocolError("frame header is truncated")
	}
	header := readOuterHeader(wire)
	if !header.hasValidConstants() {
		return Frame{}, protocolError("frame header has invalid constants")
	}
	if int(header.size) != len(wire) || header.remaining < header.size {
		return Frame{}, protocolError("frame lengths are invalid")
	}
	if CRC16(wire[crcStart:]) != header.crc {
		return Frame{}, protocolError("frame CRC does not match")
	}
	payload := append([]byte(nil), wire[outerHeaderSize:]...)
	return Frame{Sequence: header.sequence, Group: header.group, Remaining: header.remaining, Payload: payload}, nil
}

type FrameStream struct {
	buffer []byte
}

func (s *FrameStream) Feed(data []byte) ([]Frame, error) {
	s.buffer = append(s.buffer, data...)
	var frames []Frame
	for len(s.buffer) >= outerHeaderSize {
		header := readOuterHeader(s.buffer)
		if !header.hasValidConstants() {
			return nil, protocolError("stream contains an invalid frame header")
		}
		if len(s.buffer) < int(header.size) {
			break
		}
		frame, err := ParseFrame(s.buffer[:header.size])
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
		s.buffer = s.buffer[header.size:]
	}
	return frames, nil
}

func (s *FrameStream) Finish() error {
	if len(s.buffer) > 0 {
		return protocolError("connection ended inside a frame")
	}
	return nil
}

func FrameMessage(message Message, group uint32) ([]byte, error) {
	header, err := message.header()
	if err != nil {
		return nil, err
	}
	chunks := [][]byte{header}
	for start := 0; start < len(message.Body); start += sendPayloadSize {
		end := min(start+sendPayloadSize, len(message.Body))
		chunks = append(chunks, message.Body[start:end])
	}
	remaining := 0
	for _, chunk := range chunks {
		remaining += outerHeaderSize + len(chunk)
	}
	var wire []byte
	for sequence, chunk := range chunks {
		frame := Frame{Sequence: uint32(sequence), Group: group, Remaining: uint32(remaining), Payload: chunk}
		encoded, err := frame.Bytes()
		if err != nil {
			return nil, err
		}
		wire = append(wire, encoded...)
		remaining -= frame.Length()
	}
	return wire, nil
}
