package player

import (
	"time"

	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

func (d device) setPlayMode(mode int) (domain.PlayerResult, error) {
	before, err := d.readNumber(rpc.GetPlayMode)
	if err != nil {
		return domain.PlayerResult{}, err
	}
	change := &domain.PlayModeChange{Requested: domain.PlayModeName(mode), Before: domain.PlayModeName(before)}
	result := domain.PlayerResult{Action: domain.ActionSetPlayMode, PlayMode: change}
	reply, err := d.caller.Call(rpc.SetPlayMode, mode)
	if err != nil {
		return result, err
	}
	after, err := d.waitForPlayMode(mode)
	change.After = domain.PlayModeName(after)
	if err != nil {
		return result, err
	}
	if reply.Result() != commandAccepted {
		return result, notConfirmed("setPlayMode replied %d", reply.Result())
	}
	if after != mode {
		return result, notConfirmed("play mode reads %s after requesting %s", change.After, change.Requested)
	}
	result.Outcome = domain.OutcomeConfirmed
	return result, nil
}

func (d device) waitForPlayMode(mode int) (int, error) {
	deadline := time.Now().Add(d.timing.ConfirmWait)
	for {
		current, err := d.readNumber(rpc.GetPlayMode)
		if err != nil || current == mode || time.Now().After(deadline) {
			return current, err
		}
		d.waitForPoll()
	}
}
