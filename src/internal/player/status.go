package player

import (
	"setlist/internal/domain"
	"setlist/internal/hibylink/rpc"
)

type numberReader struct {
	device device
	err    error
}

func (r *numberReader) read(op rpc.Operation) int {
	if r.err != nil {
		return 0
	}
	value, err := r.device.readNumber(op)
	r.err = err
	return value
}

func (d device) model() (string, error) {
	reply, err := d.caller.Call(rpc.GetModelNumber)
	if err != nil {
		return "", err
	}
	return reply.Values.Text(1)
}

func (d device) status() (domain.PlayerResult, error) {
	model, err := d.model()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	numbers := numberReader{device: d}
	status := &domain.PlayerStatus{
		Model:      model,
		MaxVolume:  numbers.read(rpc.GetMaxVolume),
		Volume:     numbers.read(rpc.GetVolume),
		PlayState:  domain.PlayStateName(numbers.read(rpc.GetPlayState)),
		PlayMode:   domain.PlayModeName(numbers.read(rpc.GetPlayMode)),
		ProgressMs: numbers.read(rpc.GetProgress),
	}
	if numbers.err != nil {
		return domain.PlayerResult{}, numbers.err
	}
	track, err := d.currentTrack()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	status.NowPlaying = track.toDomain()
	return domain.PlayerResult{Action: domain.ActionStatus, Outcome: domain.OutcomeRead, Status: status}, nil
}
