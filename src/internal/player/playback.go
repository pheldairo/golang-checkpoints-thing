package player

import (
	"time"

	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

type playStateChange struct {
	action        domain.PlayerAction
	command       int
	requiredState int
	targetState   int
	progressMoves bool
}

var (
	pauseChange = playStateChange{
		action:        domain.ActionPause,
		command:       rpc.PlayStatePause,
		requiredState: domain.PlayStatePlaying,
		targetState:   domain.PlayStatePaused,
		progressMoves: false,
	}
	resumeChange = playStateChange{
		action:        domain.ActionResume,
		command:       rpc.PlayStateResume,
		requiredState: domain.PlayStatePaused,
		targetState:   domain.PlayStatePlaying,
		progressMoves: true,
	}
)

func (d device) toggle() (domain.PlayerResult, error) {
	state, err := d.playState()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	switch state {
	case domain.PlayStatePlaying:
		return d.changePlayState(domain.ActionToggle, pauseChange)
	case domain.PlayStatePaused:
		return d.changePlayState(domain.ActionToggle, resumeChange)
	}
	return domain.PlayerResult{}, refuse("play/pause needs the player to be playing or paused, it is %s", domain.PlayStateName(state))
}

func (d device) changePlayState(action domain.PlayerAction, change playStateChange) (domain.PlayerResult, error) {
	before, err := d.playState()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	playback := &domain.PlaybackChange{StateBefore: domain.PlayStateName(before)}
	result := domain.PlayerResult{Action: action, Playback: playback}
	if before != change.requiredState {
		return result, refuse("%s needs the player to be %s, it is %s",
			change.action, domain.PlayStateName(change.requiredState), playback.StateBefore)
	}
	reply, err := d.caller.Call(rpc.SetPlayState, change.command)
	if err != nil {
		return result, err
	}
	observed, err := d.waitForPlayState(change, playback)
	if err != nil {
		return result, err
	}
	if reply.Result() != commandAccepted {
		return result, notConfirmed("setPlayState replied %d", reply.Result())
	}
	if !observed {
		return result, notConfirmed("%s was not observed within %s, player is %s", change.action, d.timing.ConfirmWait, playback.StateAfter)
	}
	result.Outcome = domain.OutcomeConfirmed
	return result, nil
}

func (d device) waitForPlayState(change playStateChange, playback *domain.PlaybackChange) (bool, error) {
	deadline := time.Now().Add(d.timing.ConfirmWait)
	for {
		state, err := d.playState()
		if err != nil {
			return false, err
		}
		playback.StateAfter = domain.PlayStateName(state)
		if state == change.targetState {
			settled, err := d.stateSettled(change, playback)
			if err != nil {
				return false, err
			}
			if settled {
				return true, nil
			}
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		d.waitForPoll()
	}
}

func (d device) stateSettled(change playStateChange, playback *domain.PlaybackChange) (bool, error) {
	moving, err := d.progressIsMoving(playback)
	if err != nil || moving != change.progressMoves {
		return false, err
	}
	state, err := d.playState()
	return state == change.targetState, err
}

func (d device) progressIsMoving(playback *domain.PlaybackChange) (bool, error) {
	first, err := d.progress()
	if err != nil {
		return false, err
	}
	d.waitForProbe()
	second, err := d.progress()
	if err != nil {
		return false, err
	}
	playback.ProgressSamplesMs = append(playback.ProgressSamplesMs, first, second)
	return second-first > frozenProgressMs, nil
}
