package player

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"setlist/internal/hibylink/rpc"
)

var testTiming = Timing{ConfirmWait: 60 * time.Millisecond, PollInterval: time.Millisecond, ProgressProbe: time.Millisecond}

type handler func(args []any) rpc.Reply

type stubPlayer struct {
	lock     sync.Mutex
	handlers map[string]handler
	sent     []string
	usable   bool
	closed   bool
}

func newStub(handlers map[string]handler) *stubPlayer {
	return &stubPlayer{handlers: handlers, usable: true}
}

func (s *stubPlayer) Call(op rpc.Operation, args ...any) (rpc.Reply, error) {
	if op.Validate != nil {
		if err := op.Validate(args); err != nil {
			return rpc.Reply{}, err
		}
	}
	s.lock.Lock()
	s.sent = append(s.sent, op.Name)
	respond, known := s.handlers[op.Name]
	s.lock.Unlock()
	if !known {
		return rpc.Reply{}, fmt.Errorf("stub has no answer for %s", op.Name)
	}
	return respond(args), nil
}

func (s *stubPlayer) Usable() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.usable && !s.closed
}

func (s *stubPlayer) Close() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.closed = true
}

func (s *stubPlayer) wasSent(name string) bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return slices.Contains(s.sent, name)
}

func (s *stubPlayer) device() device {
	return device{caller: s, timing: testTiming}
}

func number(value int) rpc.Reply {
	return rpc.Reply{Signature: "i", Values: []any{int32(value)}}
}

func constant(value int) handler {
	return func([]any) rpc.Reply { return number(value) }
}

func reading(value *int) handler {
	return func([]any) rpc.Reply { return number(*value) }
}

func advancing(start, step int) handler {
	position := start
	return func([]any) rpc.Reply {
		position += step
		return number(position)
	}
}

func trackRow(path string, durationMs, format int) []any {
	return []any{
		[]byte("Title " + path), []byte("Artist"), []byte("Album"), []byte(""), []byte(""), []byte(path), []byte(""),
		uint32(durationMs), uint32(0), uint32(0), uint32(44100), uint16(format), uint8(16), uint8(2), uint8(0),
	}
}

func metadata(path *string, durationMs, format int) handler {
	return func([]any) rpc.Reply {
		return rpc.Reply{Signature: rpc.MetadataSignature, Values: []any{int32(0), [][]any{trackRow(*path, durationMs, format)}}}
	}
}

func libraryRow(rowid int) []any {
	return []any{
		[]byte(fmt.Sprintf("T%d", rowid)), []byte("A"),
		[]byte(fmt.Sprintf(`<path>a:\%d.flac<cue_id>-1<name>T%d`, rowid, rowid)),
		uint32(rowid), uint32(68), uint8(2), uint8(16),
	}
}

func libraryOf(rowids []int) handler {
	return func(args []any) rpc.Reply {
		start, end := args[1].(int), args[2].(int)
		var rows [][]any
		for _, rowid := range rowids {
			if rowid > start && len(rows) < end-start+1 {
				rows = append(rows, libraryRow(rowid))
			}
		}
		if len(rows) == 0 {
			return number(0)
		}
		return rpc.Reply{Signature: rpc.LibraryItemsSignature, Values: []any{int32(len(rows)), rows}}
	}
}

func rowids(first, last int) []int {
	var ids []int
	for id := first; id <= last; id++ {
		ids = append(ids, id)
	}
	return ids
}
