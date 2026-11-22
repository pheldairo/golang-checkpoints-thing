package player

import (
	"errors"
	"fmt"
	"time"

	"setlist/internal/playerlink/protocol"
	"setlist/internal/playerlink/rpc"
)

var (
	ErrRefused      = errors.New("player command refused before sending")
	ErrNotConfirmed = errors.New("player did not confirm the command")
)

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
}

func notConfirmed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotConfirmed, fmt.Sprintf(format, args...))
}

type Caller interface {
	Call(op rpc.Operation, args ...any) (rpc.Reply, error)
}

type Timing struct {
	ConfirmWait   time.Duration
	PollInterval  time.Duration
	ProgressProbe time.Duration
}

func DefaultTiming() Timing {
	return Timing{
		ConfirmWait:   15 * time.Second,
		PollInterval:  300 * time.Millisecond,
		ProgressProbe: 700 * time.Millisecond,
	}
}

const (
	restartThresholdMs = 3000
	frozenProgressMs   = 150
	maxVolumeIncrease  = 5
	commandAccepted    = 0
	navigationQueued   = 1
)

type device struct {
	caller Caller
	timing Timing
}

func (d device) readNumber(op rpc.Operation) (int, error) {
	reply, err := d.caller.Call(op)
	if err != nil {
		return 0, err
	}
	if reply.Signature != "i" || len(reply.Values) != 1 {
		return 0, fmt.Errorf("%w: %s reply has an unexpected shape", protocol.ErrProtocol, op.Name)
	}
	if reply.Result() < 0 {
		return 0, fmt.Errorf("%s returned error code %d", op.Name, reply.Result())
	}
	return reply.Result(), nil
}

func (d device) playState() (int, error) {
	return d.readNumber(rpc.GetPlayState)
}

func (d device) progress() (int, error) {
	return d.readNumber(rpc.GetProgress)
}

func (d device) waitForPoll() {
	time.Sleep(d.timing.PollInterval)
}

func (d device) waitForProbe() {
	time.Sleep(d.timing.ProgressProbe)
}

func millisecondsSince(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}

func resetBoundMs(commandSent time.Time) int {
	return max(restartThresholdMs, millisecondsSince(commandSent)+1000)
}
