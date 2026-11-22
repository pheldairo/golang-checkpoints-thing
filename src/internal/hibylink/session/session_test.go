package session

import (
	"errors"
	"testing"
	"time"

	"setlist/internal/hibylink/rpc"
	"setlist/internal/hibylink/scripted"
)

func fastConfig(server *scripted.Server) Config {
	config := DefaultConfig("127.0.0.1")
	config.DataPort = server.DataPort()
	config.HeartbeatPort = server.HeartbeatPort()
	config.RPCTimeout = time.Second
	config.HeartbeatInterval = 20 * time.Millisecond
	config.HeartbeatTimeout = 150 * time.Millisecond
	return config
}

func playerWithVolume(volume int) scripted.Handler {
	return func(call scripted.Call) scripted.Reply {
		if reply, ok := scripted.ModelReply(call); ok {
			return reply
		}
		return scripted.Reply{Signature: "i", Values: []any{volume}}
	}
}

func waitUntil(t *testing.T, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOpenChecksModelAndKeepsHeartbeatAlive(t *testing.T) {
	server := scripted.Start(t, playerWithVolume(30))
	session, err := Open(fastConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	reply, err := session.Call(rpc.GetVolume)
	if err != nil || reply.Result() != 30 {
		t.Fatalf("reply %+v, %v", reply, err)
	}
	waitUntil(t, func() bool { return session.HeartbeatsAnswered() >= 4 }, "periodic heartbeats")
	if !session.Usable() {
		t.Fatalf("healthy session reported unusable: %v", session.Err())
	}
}

func TestOpenRejectsAnUnexpectedModel(t *testing.T) {
	server := scripted.Start(t, func(scripted.Call) scripted.Reply {
		return scripted.Reply{Signature: "is", Values: []any{0, "other"}}
	})
	if _, err := Open(fastConfig(server)); !errors.Is(err, ErrUnusable) {
		t.Fatalf("unexpected model accepted: %v", err)
	}
}

func TestSilentHeartbeatFailsTheSession(t *testing.T) {
	server := scripted.Start(t, playerWithVolume(30))
	session, err := Open(fastConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	server.SilentHeartbeat.Store(true)
	waitUntil(t, func() bool { return !session.Usable() }, "heartbeat failure")
	if _, err := session.Call(rpc.GetVolume); !errors.Is(err, ErrUnusable) {
		t.Fatalf("failed session still accepted a call: %v", err)
	}
}

func TestCloseIsIdempotentAndStopsTheHeartbeat(t *testing.T) {
	server := scripted.Start(t, playerWithVolume(30))
	session, err := Open(fastConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		session.Close()
		session.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return, heartbeat goroutine still running")
	}
	answered := session.HeartbeatsAnswered()
	time.Sleep(60 * time.Millisecond)
	if session.HeartbeatsAnswered() != answered {
		t.Fatal("heartbeats continued after Close")
	}
	if _, err := session.Call(rpc.GetVolume); !errors.Is(err, ErrUnusable) {
		t.Fatalf("closed session accepted a call: %v", err)
	}
}

func TestConcurrentCallsAreSerialized(t *testing.T) {
	server := scripted.Start(t, playerWithVolume(12))
	session, err := Open(fastConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	results := make(chan error, 20)
	for range 20 {
		go func() {
			reply, err := session.Call(rpc.GetVolume)
			if err == nil && reply.Result() != 12 {
				err = errors.New("wrong reply")
			}
			results <- err
		}()
	}
	for range 20 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
