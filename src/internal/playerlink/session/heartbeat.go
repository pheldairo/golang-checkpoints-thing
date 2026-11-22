package session

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"setlist/internal/playerlink/protocol"
)

var (
	heartbeatRequest  = []byte("HLREQ: HEARTBEAT")
	heartbeatResponse = []byte("HLRSP: TAEBTRAEH")
)

const (
	maxUnansweredHeartbeats = 30
	heartbeatReadSlice      = 200 * time.Millisecond
	minimumHeartbeatWait    = 10 * time.Millisecond
)

type heartbeat struct {
	conn     net.Conn
	timeout  time.Duration
	received []byte
}

func (h *heartbeat) sendRequest() error {
	if err := h.conn.SetWriteDeadline(time.Now().Add(h.timeout)); err != nil {
		return err
	}
	_, err := h.conn.Write(heartbeatRequest)
	return err
}

func (h *heartbeat) exchange() error {
	if err := h.sendRequest(); err != nil {
		return err
	}
	if err := h.conn.SetReadDeadline(time.Now().Add(h.timeout)); err != nil {
		return err
	}
	response := make([]byte, len(heartbeatResponse))
	if _, err := io.ReadFull(h.conn, response); err != nil {
		return fmt.Errorf("heartbeat response: %w", err)
	}
	if !bytes.Equal(response, heartbeatResponse) {
		return fmt.Errorf("%w: unexpected heartbeat response %q", protocol.ErrProtocol, response)
	}
	return nil
}

func (h *heartbeat) readResponses(wait time.Duration) (int, error) {
	if err := h.conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		return 0, err
	}
	chunk := make([]byte, 256)
	count, err := h.conn.Read(chunk)
	if isTimeout(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	h.received = append(h.received, chunk[:count]...)
	responses := 0
	for len(h.received) >= len(heartbeatResponse) {
		if !bytes.Equal(h.received[:len(heartbeatResponse)], heartbeatResponse) {
			return responses, fmt.Errorf("%w: unexpected heartbeat response", protocol.ErrProtocol)
		}
		h.received = h.received[len(heartbeatResponse):]
		responses++
	}
	return responses, nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (s *Session) runHeartbeat() {
	defer s.heartbeatStopped.Done()
	var unanswered []time.Time
	nextSend := time.Now().Add(s.config.HeartbeatInterval)
	for !s.stopping() {
		if !time.Now().Before(nextSend) {
			if err := s.sendScheduledHeartbeat(&unanswered); err != nil {
				s.heartbeatFailed(err)
				return
			}
			nextSend = time.Now().Add(s.config.HeartbeatInterval)
		}
		wait := min(heartbeatReadSlice, max(minimumHeartbeatWait, time.Until(nextSend)))
		if len(unanswered) == 0 {
			s.sleepUnlessStopped(wait)
			continue
		}
		if err := s.collectHeartbeatResponses(&unanswered, wait); err != nil {
			s.heartbeatFailed(err)
			return
		}
	}
}

func (s *Session) sendScheduledHeartbeat(unanswered *[]time.Time) error {
	if len(*unanswered) >= maxUnansweredHeartbeats {
		return nil
	}
	if err := s.heartbeat.sendRequest(); err != nil {
		return err
	}
	*unanswered = append(*unanswered, time.Now())
	return nil
}

func (s *Session) collectHeartbeatResponses(unanswered *[]time.Time, wait time.Duration) error {
	answered, err := s.heartbeat.readResponses(wait)
	if err != nil {
		return err
	}
	answered = min(answered, len(*unanswered))
	*unanswered = (*unanswered)[answered:]
	s.heartbeatsAnswered.Add(int64(answered))
	if len(*unanswered) > 0 && time.Since((*unanswered)[0]) > s.config.HeartbeatTimeout {
		return fmt.Errorf("no heartbeat response for %s (%d unanswered)", s.config.HeartbeatTimeout, len(*unanswered))
	}
	return nil
}

func (s *Session) heartbeatFailed(err error) {
	if s.stopping() {
		return
	}
	s.fail(fmt.Errorf("heartbeat failed: %w", err))
}
