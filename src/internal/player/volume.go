package player

import (
	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

func (d device) setVolume(input domain.PlayerInput) (domain.PlayerResult, error) {
	numbers := numberReader{device: d}
	maximum := numbers.read(rpc.GetMaxVolume)
	baseline := numbers.read(rpc.GetVolume)
	if numbers.err != nil {
		return domain.PlayerResult{}, numbers.err
	}
	target, err := volumeTarget(input, baseline, maximum)
	if err != nil {
		return domain.PlayerResult{}, err
	}
	change := &domain.VolumeChange{Requested: target, Before: baseline, After: baseline, Maximum: maximum}
	result := domain.PlayerResult{Action: domain.ActionSetVolume, Volume: change}
	if target == baseline {
		result.Outcome = domain.OutcomeAlreadyApplied
		return result, nil
	}
	reply, err := d.caller.Call(rpc.SetVolume, target)
	if err != nil {
		return result, err
	}
	change.After, err = d.readNumber(rpc.GetVolume)
	if err != nil {
		return result, err
	}
	if reply.Result() != commandAccepted {
		return result, notConfirmed("setVolume replied %d", reply.Result())
	}
	if change.After != target {
		return result, notConfirmed("volume reads %d after setting %d", change.After, target)
	}
	result.Outcome = domain.OutcomeConfirmed
	return result, nil
}

func volumeTarget(input domain.PlayerInput, baseline, maximum int) (int, error) {
	if maximum <= 0 || maximum > 0xff {
		return 0, refuse("player maximum volume %d does not fit the one-byte setter", maximum)
	}
	if baseline > maximum {
		return 0, refuse("current volume %d is above the player maximum %d", baseline, maximum)
	}
	target := input.Volume
	if input.RelativeVolume {
		target = min(max(baseline+input.Volume, 0), maximum)
	}
	if target < 0 || target > maximum {
		return 0, refuse("volume %d is outside the player range 0..%d", target, maximum)
	}
	if target-baseline > maxVolumeIncrease {
		return 0, refuse("raising volume from %d to %d is more than %d steps at once", baseline, target, maxVolumeIncrease)
	}
	return target, nil
}
