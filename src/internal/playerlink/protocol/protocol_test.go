package protocol

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

const knownModelRequest = "55aaa55a356e00012a000000000000000b00000046000000a5a56c0101010b000400000002000000750055aaa55a7d2800011c000000010000000b0000001c000000021001c5"

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func modelCall(t *testing.T) Message {
	t.Helper()
	call, err := NewCall(0xc501, 0x1002, 11, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func readMessages(wire []byte, chunkSize int) ([]Message, error) {
	var stream FrameStream
	reassembler := NewReassembler()
	var messages []Message
	for start := 0; start < len(wire); start += chunkSize {
		frames, err := stream.Feed(wire[start:min(start+chunkSize, len(wire))])
		if err != nil {
			return nil, err
		}
		for _, frame := range frames {
			message, err := reassembler.Feed(frame)
			if err != nil {
				return nil, err
			}
			if message != nil {
				messages = append(messages, *message)
			}
		}
	}
	if err := stream.Finish(); err != nil {
		return nil, err
	}
	return messages, reassembler.Finish()
}

func TestCRCMatchesIndependentVectors(t *testing.T) {
	if CRC16(nil) != 0xffff {
		t.Fatalf("empty CRC is %#x", CRC16(nil))
	}
	if CRC16([]byte("123456789")) != 0x4b37 {
		t.Fatalf("check value is %#x", CRC16([]byte("123456789")))
	}
}

func TestCodecPacksFieldsLikeTheFirmware(t *testing.T) {
	wire := []byte("\x7f\x34\x12\xfe\xff\xff\xff\x03\x00\x00\x00A\xff\x00")
	encoded, err := Encode("yqis", []any{127, 0x1234, -2, []byte("A\xff")})
	if err != nil || !bytes.Equal(encoded, wire) {
		t.Fatalf("encoded %x, %v", encoded, err)
	}
	decoded, err := Decode("yqis", wire)
	want := Values{uint8(127), uint16(0x1234), int32(-2), []byte("A\xff")}
	if err != nil || !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded %#v, %v", decoded, err)
	}
}

func TestArrayCountsAreElementCounts(t *testing.T) {
	wire := mustHex(t, "0200000001000000ffffffff")
	decoded, err := Decode("au", wire)
	if err != nil || !reflect.DeepEqual(decoded, Values{[]any{uint32(1), uint32(0xffffffff)}}) {
		t.Fatalf("decoded %#v, %v", decoded, err)
	}
	structWire := mustHex(t, "0200000002000000020000006100341201000000000900"+"01c5")
	values := []any{2, [][]any{{[]byte("a"), 0x1234}, {[]byte(""), 9}}, 0xc501}
	encoded, err := Encode("ia(sq)q", values)
	if err != nil || !bytes.Equal(encoded, structWire) {
		t.Fatalf("encoded %x, %v", encoded, err)
	}
	rows, err := Decode("ia(sq)q", structWire)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := rows.Rows(1)
	if err != nil || len(parsed) != 2 {
		t.Fatalf("rows %#v, %v", parsed, err)
	}
	title, _ := parsed[0].Text(0)
	number, _ := parsed[0].Int(1)
	if title != "a" || number != 0x1234 {
		t.Fatalf("first row is %q %#x", title, number)
	}
}

func TestOpaqueFieldsStayRawBytes(t *testing.T) {
	body := make([]byte, 24)
	for index := range body {
		body[index] = byte(index)
	}
	decoded, err := Decode("xtd", body)
	if err != nil {
		t.Fatal(err)
	}
	first := decoded[0].([8]byte)
	if !bytes.Equal(first[:], body[:8]) {
		t.Fatalf("first opaque value is %x", first)
	}
}

func TestInvalidSignaturesAndBodiesAreRejected(t *testing.T) {
	for _, signature := range []string{"a", "as", "a()", "a(ay)", "a(s", "z"} {
		if _, err := parseSignature(signature); !errors.Is(err, ErrProtocol) {
			t.Errorf("signature %q was accepted", signature)
		}
	}
	bodies := []struct {
		signature string
		body      []byte
	}{
		{"s", []byte{0, 0, 0, 0}},
		{"u", []byte{1}},
		{"", []byte("x")},
		{"s", []byte("\x02\x00\x00\x00ab")},
		{"au", []byte{0xff, 0xff, 0xff, 0xff}},
	}
	for _, testCase := range bodies {
		if _, err := Decode(testCase.signature, testCase.body); !errors.Is(err, ErrProtocol) {
			t.Errorf("body %x for %q was accepted", testCase.body, testCase.signature)
		}
	}
	if _, err := Encode("y", []any{256}); !errors.Is(err, ErrProtocol) {
		t.Error("out of range byte was accepted")
	}
	if _, err := Encode("s", []any{"a\x00b"}); !errors.Is(err, ErrProtocol) {
		t.Error("string with NUL was accepted")
	}
}

func TestModelRequestMatchesArchivedBytes(t *testing.T) {
	expected := mustHex(t, knownModelRequest)
	wire, err := FrameMessage(modelCall(t), 11)
	if err != nil || !bytes.Equal(wire, expected) {
		t.Fatalf("framed %x, %v", wire, err)
	}
	for chunkSize := 1; chunkSize <= len(expected); chunkSize++ {
		messages, err := readMessages(expected, chunkSize)
		if err != nil || len(messages) != 1 || !reflect.DeepEqual(messages[0], modelCall(t)) {
			t.Fatalf("chunk size %d: %+v, %v", chunkSize, messages, err)
		}
	}
}

func TestLargeBodiesSplitAndInterleavedGroupsReassemble(t *testing.T) {
	art := bytes.Repeat([]byte{1, 2, 3, 4, 5}, 1000)
	cover, err := NewMessage(KindReply, 11, "iay", []any{5000, art})
	if err != nil {
		t.Fatal(err)
	}
	volume, err := NewMessage(KindNotification, 12, "uy", []any{0xc5021105, 73})
	if err != nil {
		t.Fatal(err)
	}
	coverFrames := framesOf(t, cover, 20)
	volumeFrames := framesOf(t, volume, 21)
	if len(coverFrames) < 3 {
		t.Fatalf("cover used only %d frames", len(coverFrames))
	}
	var wire []byte
	for _, frame := range append(append([]Frame{coverFrames[0]}, volumeFrames...), coverFrames[1:]...) {
		encoded, err := frame.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		wire = append(wire, encoded...)
	}
	messages, err := readMessages(wire, 7)
	if err != nil || len(messages) != 2 {
		t.Fatalf("%d messages, %v", len(messages), err)
	}
	if !reflect.DeepEqual(messages[0], volume) || !reflect.DeepEqual(messages[1], cover) {
		t.Fatal("interleaved messages were reassembled in the wrong order or form")
	}
}

func framesOf(t *testing.T, message Message, group uint32) []Frame {
	t.Helper()
	wire, err := FrameMessage(message, group)
	if err != nil {
		t.Fatal(err)
	}
	var stream FrameStream
	frames, err := stream.Feed(wire)
	if err != nil {
		t.Fatal(err)
	}
	return frames
}

func TestCorruptedAndTruncatedStreamsFail(t *testing.T) {
	valid := mustHex(t, knownModelRequest)
	corrupted := bytes.Clone(valid)
	corrupted[len(corrupted)-1] ^= 1
	if _, err := readMessages(corrupted, 100); !errors.Is(err, ErrProtocol) {
		t.Fatalf("corrupted CRC accepted: %v", err)
	}
	for _, cut := range []int{1, 23, 24, 41, 42, 69} {
		if _, err := readMessages(valid[:cut], 10); !errors.Is(err, ErrProtocol) {
			t.Errorf("stream cut at %d was accepted", cut)
		}
	}
	frames := framesOf(t, modelCall(t), 11)
	reassembler := NewReassembler()
	if _, err := reassembler.Feed(frames[0]); err != nil {
		t.Fatal(err)
	}
	outOfOrder := Frame{Sequence: 2, Group: 11, Remaining: 28, Payload: frames[1].Payload}
	if _, err := reassembler.Feed(outOfOrder); !errors.Is(err, ErrProtocol) {
		t.Fatalf("out of order frame accepted: %v", err)
	}
}
