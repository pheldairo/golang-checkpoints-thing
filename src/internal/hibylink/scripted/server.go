package scripted

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"setlist/internal/hibylink/protocol"
)

var (
	heartbeatRequest  = []byte("HLREQ: HEARTBEAT")
	heartbeatResponse = []byte("HLRSP: TAEBTRAEH")
)

type Call struct {
	Service uint16
	Action  uint16
	Serial  uint16
	Args    protocol.Values
}

type Reply struct {
	Signature   string
	Values      []any
	SerialShift uint16
}

type Handler func(Call) Reply

type Server struct {
	handler         Handler
	data            net.Listener
	heartbeat       net.Listener
	connections     sync.WaitGroup
	openConnsLock   sync.Mutex
	openConns       []net.Conn
	SilentHeartbeat atomic.Bool
	Calls           atomic.Int64
	Heartbeats      atomic.Int64
}

func Start(t testing.TB, handler Handler) *Server {
	t.Helper()
	server := &Server{handler: handler}
	server.data = listen(t)
	server.heartbeat = listen(t)
	server.connections.Add(2)
	go server.accept(server.data, server.serveData)
	go server.accept(server.heartbeat, server.serveHeartbeat)
	t.Cleanup(server.Close)
	return server
}

func listen(t testing.TB) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener
}

func (s *Server) DataPort() int {
	return s.data.Addr().(*net.TCPAddr).Port
}

func (s *Server) HeartbeatPort() int {
	return s.heartbeat.Addr().(*net.TCPAddr).Port
}

func (s *Server) Close() {
	_ = s.data.Close()
	_ = s.heartbeat.Close()
	s.openConnsLock.Lock()
	for _, conn := range s.openConns {
		_ = conn.Close()
	}
	s.openConnsLock.Unlock()
	s.connections.Wait()
}

func (s *Server) accept(listener net.Listener, serve func(net.Conn)) {
	defer s.connections.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		s.openConnsLock.Lock()
		s.openConns = append(s.openConns, conn)
		s.openConnsLock.Unlock()
		s.connections.Add(1)
		go func() {
			defer s.connections.Done()
			defer conn.Close()
			serve(conn)
		}()
	}
}

func (s *Server) serveHeartbeat(conn net.Conn) {
	var pending []byte
	chunk := make([]byte, 256)
	for {
		count, err := conn.Read(chunk)
		if err != nil {
			return
		}
		pending = append(pending, chunk[:count]...)
		for len(pending) >= len(heartbeatRequest) {
			if !bytes.Equal(pending[:len(heartbeatRequest)], heartbeatRequest) {
				return
			}
			pending = pending[len(heartbeatRequest):]
			if s.SilentHeartbeat.Load() {
				continue
			}
			s.Heartbeats.Add(1)
			if _, err := conn.Write(heartbeatResponse); err != nil {
				return
			}
		}
	}
}

func (s *Server) serveData(conn net.Conn) {
	var stream protocol.FrameStream
	reassembler := protocol.NewReassembler()
	chunk := make([]byte, 64*1024)
	for {
		count, err := conn.Read(chunk)
		if err != nil {
			return
		}
		frames, err := stream.Feed(chunk[:count])
		if err != nil {
			return
		}
		for _, frame := range frames {
			message, err := reassembler.Feed(frame)
			if err != nil {
				return
			}
			if message != nil && s.answer(conn, *message) != nil {
				return
			}
		}
	}
}

func (s *Server) answer(conn net.Conn, message protocol.Message) error {
	values, err := message.Values()
	if err != nil {
		return err
	}
	dispatch, err := values.Int(0)
	if err != nil {
		return err
	}
	s.Calls.Add(1)
	call := Call{Service: uint16(dispatch >> 16), Action: uint16(dispatch), Serial: message.Serial, Args: values[1:]}
	reply := s.handler(call)
	if reply.Signature == "" {
		return errors.New("handler chose to drop the connection")
	}
	answer, err := protocol.NewMessage(protocol.KindReply, message.Serial+reply.SerialShift, reply.Signature, reply.Values)
	if err != nil {
		return err
	}
	wire, err := protocol.FrameMessage(answer, uint32(message.Serial)+1000)
	if err != nil {
		return err
	}
	_, err = conn.Write(wire)
	return err
}

func ModelReply(call Call) (Reply, bool) {
	if call.Service == 0xc501 && call.Action == 0x1002 {
		return Reply{Signature: "is", Values: []any{0, "hm001"}}, true
	}
	return Reply{}, false
}
