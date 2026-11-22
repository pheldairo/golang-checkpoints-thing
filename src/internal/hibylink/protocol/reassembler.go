package protocol

import "encoding/binary"

const maxEncodedMessage = MaxBodySize + MaxBodySize/sendPayloadSize*outerHeaderSize + 4096

type partialMessage struct {
	nextSequence uint32
	remaining    uint32
	header       []byte
	body         []byte
	expectedBody uint32
}

type Reassembler struct {
	groups map[uint32]*partialMessage
}

func NewReassembler() *Reassembler {
	return &Reassembler{groups: make(map[uint32]*partialMessage)}
}

func (r *Reassembler) Feed(frame Frame) (*Message, error) {
	partial, known := r.groups[frame.Group]
	if !known {
		started, err := r.startGroup(frame)
		if err != nil {
			return nil, err
		}
		partial = started
	}
	if frame.Sequence != partial.nextSequence || frame.Remaining != partial.remaining {
		return nil, protocolError("frame sequence or remaining length is out of order")
	}
	if frame.Sequence > 0 {
		partial.body = append(partial.body, frame.Payload...)
	}
	if uint32(len(partial.body)) > partial.expectedBody {
		return nil, protocolError("message body is longer than its header declares")
	}
	partial.nextSequence++
	partial.remaining -= uint32(frame.Length())
	if partial.remaining == 0 {
		delete(r.groups, frame.Group)
		message, err := messageFromParts(partial.header, partial.body)
		if err != nil {
			return nil, err
		}
		return &message, nil
	}
	if len(frame.Payload) == 0 {
		return nil, protocolError("a non-final frame has an empty payload")
	}
	return nil, nil
}

func (r *Reassembler) startGroup(frame Frame) (*partialMessage, error) {
	if frame.Sequence != 0 || len(r.groups) >= maxPendingGroups {
		return nil, protocolError("unexpected start of message group %d", frame.Group)
	}
	if frame.Remaining > maxEncodedMessage {
		return nil, protocolError("encoded message is too large")
	}
	if len(frame.Payload) < innerHeaderSize+1 {
		return nil, protocolError("first frame is too short for a message header")
	}
	bodySize := binary.LittleEndian.Uint32(frame.Payload[8:12])
	signatureSize := binary.LittleEndian.Uint32(frame.Payload[12:16])
	if bodySize > MaxBodySize || int(signatureSize) != len(frame.Payload)-innerHeaderSize {
		return nil, protocolError("first frame declares invalid message lengths")
	}
	partial := &partialMessage{
		remaining:    frame.Remaining,
		header:       frame.Payload,
		expectedBody: bodySize,
	}
	r.groups[frame.Group] = partial
	return partial, nil
}

func (r *Reassembler) Finish() error {
	if len(r.groups) > 0 {
		return protocolError("connection ended inside a message")
	}
	return nil
}
