package session

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"setlist/internal/playerlink/rpc"
)

var ErrUnusable = errors.New("player session is not usable")

const (
	DefaultDataPort      = 4001
	DefaultHeartbeatPort = 4002
	expectedModel        = "hm001"
)

type Config struct {
	Host              string
	DataPort          int
	HeartbeatPort     int
	RPCTimeout        time.Duration
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

func DefaultConfig(host string) Config {
	return Config{
		Host:              host,
		DataPort:          DefaultDataPort,
		HeartbeatPort:     DefaultHeartbeatPort,
		RPCTimeout:        12 * time.Second,
		HeartbeatInterval: time.Second,
		HeartbeatTimeout:  10 * time.Second,
	}
}

func (c Config) address(port int) string {
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

type Session struct {
	config             Config
	rpcLock            sync.Mutex
	client             *rpc.Client
	dataConn           net.Conn
	heartbeat          *heartbeat
	stateLock          sync.Mutex
	failure            error
	closed             bool
	stop               chan struct{}
	heartbeatStopped   sync.WaitGroup
	closeOnce          sync.Once
	heartbeatsAnswered atomic.Int64
}

func Open(config Config) (*Session, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("%w: a player host is required", ErrUnusable)
	}
	session := &Session{config: config, stop: make(chan struct{})}
	if err := session.connect(); err != nil {
		session.closeConnections()
		return nil, err
	}
	session.heartbeatStopped.Add(1)
	go session.runHeartbeat()
	return session, nil
}

func (s *Session) connect() error {
	dataConn, err := dial(s.config.address(s.config.DataPort), s.config.RPCTimeout)
	if err != nil {
		return fmt.Errorf("connect to player data port: %w", err)
	}
	s.dataConn = dataConn
	s.client = rpc.NewClient(dataConn, s.config.RPCTimeout)
	if err := s.checkModel(); err != nil {
		return err
	}
	heartbeatConn, err := dial(s.config.address(s.config.HeartbeatPort), s.config.HeartbeatTimeout)
	if err != nil {
		return fmt.Errorf("connect to player heartbeat port: %w", err)
	}
	s.heartbeat = &heartbeat{conn: heartbeatConn, timeout: s.config.HeartbeatTimeout}
	if err := s.heartbeat.exchange(); err != nil {
		return fmt.Errorf("first heartbeat: %w", err)
	}
	s.heartbeatsAnswered.Add(1)
	return nil
}

func (s *Session) checkModel() error {
	reply, err := s.client.Call(rpc.GetModelNumber)
	if err != nil {
		return err
	}
	model, err := reply.Values.Text(1)
	if err != nil || reply.Result() != 0 || model != expectedModel {
		return fmt.Errorf("%w: unexpected player model reply %v", ErrUnusable, reply.Values)
	}
	return nil
}

func (s *Session) Call(op rpc.Operation, args ...any) (rpc.Reply, error) {
	s.rpcLock.Lock()
	defer s.rpcLock.Unlock()
	if err := s.Err(); err != nil {
		return rpc.Reply{}, err
	}
	reply, err := s.client.Call(op, args...)
	if err != nil && s.client.Failure() != nil {
		s.fail(err)
	}
	return reply, err
}

func (s *Session) Err() error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if s.closed {
		return fmt.Errorf("%w: session is closed", ErrUnusable)
	}
	if s.failure != nil {
		return fmt.Errorf("%w: %v", ErrUnusable, s.failure)
	}
	return nil
}

func (s *Session) Usable() bool {
	return s.Err() == nil
}

func (s *Session) HeartbeatsAnswered() int64 {
	return s.heartbeatsAnswered.Load()
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.stateLock.Lock()
		s.closed = true
		s.stateLock.Unlock()
		close(s.stop)
		s.closeConnections()
		s.heartbeatStopped.Wait()
	})
}

func (s *Session) closeConnections() {
	if s.dataConn != nil {
		if tcp, ok := s.dataConn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = s.dataConn.Close()
	}
	if s.heartbeat != nil {
		_ = s.heartbeat.conn.Close()
	}
}

func (s *Session) fail(err error) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if s.failure == nil && !s.closed {
		s.failure = err
	}
}

func (s *Session) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *Session) sleepUnlessStopped(wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-s.stop:
	case <-timer.C:
	}
}
