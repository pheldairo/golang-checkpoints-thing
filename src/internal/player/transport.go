package player

import (
	"time"

	"setlist/internal/domain"
	"setlist/internal/hibylink/rpc"
)

const (
	outcomeChanged   = "changed"
	outcomeRestarted = "restarted"
	outcomePending   = "pending"
	outcomeUnchanged = "unchanged"
)

type trackSwitch struct {
	action        domain.PlayerAction
	operation     rpc.Operation
	args          []any
	expectedReply int
	sameTrack     bool
}

func (d device) next() (domain.PlayerResult, error) {
	return d.switchTrack(trackSwitch{action: domain.ActionNext, operation: rpc.PlayNext, expectedReply: navigationQueued})
}

func (d device) previous() (domain.PlayerResult, error) {
	return d.switchTrack(trackSwitch{action: domain.ActionPrevious, operation: rpc.PlayPrevious, expectedReply: navigationQueued})
}

func (d device) restart() (domain.PlayerResult, error) {
	state, err := d.playState()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	if state != domain.PlayStatePlaying && state != domain.PlayStatePaused {
		return domain.PlayerResult{}, refuse("restart needs the player to be playing or paused, it is %s", domain.PlayStateName(state))
	}
	return d.switchTrack(trackSwitch{
		action:        domain.ActionRestart,
		operation:     rpc.SetProgress,
		args:          []any{0},
		expectedReply: commandAccepted,
		sameTrack:     true,
	})
}

func (d device) switchTrack(request trackSwitch) (domain.PlayerResult, error) {
	before, err := d.currentTrack()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	change := &domain.TrackChange{Before: before.toDomain()}
	result := domain.PlayerResult{Action: request.action, Track: change}
	sentAt := time.Now()
	reply, err := d.caller.Call(request.operation, request.args...)
	if err != nil {
		return result, err
	}
	after, progress, err := d.waitForTrackSwitch(before, sentAt, request.sameTrack)
	if err != nil {
		return result, err
	}
	change.After = after.toDomain()
	change.ProgressAfterMs = progress
	outcome := describeTrackSwitch(before, after, progress < resetBoundMs(sentAt), request.sameTrack)
	if reply.Result() != request.expectedReply {
		return result, notConfirmed("%s replied %d", request.operation.Name, reply.Result())
	}
	wanted := outcomeChanged
	if request.sameTrack {
		wanted = outcomeRestarted
	}
	if outcome != wanted {
		return result, notConfirmed("expected the track to be %s, observed %s within %s", wanted, outcome, d.timing.ConfirmWait)
	}
	result.Outcome = outcome
	return result, nil
}

func (d device) waitForTrackSwitch(before *trackRecord, sentAt time.Time, sameTrack bool) (*trackRecord, int, error) {
	deadline := sentAt.Add(d.timing.ConfirmWait)
	for {
		d.waitForPoll()
		after, err := d.currentTrack()
		if err != nil {
			return nil, 0, err
		}
		progress, err := d.progress()
		if err != nil {
			return nil, 0, err
		}
		progressReset := progress < resetBoundMs(sentAt)
		if progressReset && sameFile(after, before) == sameTrack {
			return after, progress, nil
		}
		if time.Now().After(deadline) {
			return after, progress, nil
		}
	}
}

func describeTrackSwitch(before, after *trackRecord, progressReset, sameTrack bool) string {
	if !sameFile(after, before) {
		if sameTrack || progressReset {
			return outcomeChanged
		}
		return outcomePending
	}
	if sameTrack && progressReset {
		return outcomeRestarted
	}
	return outcomeUnchanged
}
