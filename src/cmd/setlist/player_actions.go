package main

import (
	"fmt"
	"strconv"
	"strings"

	"setlist/internal/domain"
)

const defaultPlayerActions = "status,volume-down,mode:2,play-track:1,seek:30000,toggle,library:5"

var optionalNumberDefaults = map[string]int{
	"seek":       30000,
	"mode":       domain.PlayModeRepeatAll,
	"play-track": 0,
	"library":    10,
}

var simplePlayerActions = map[string]domain.PlayerAction{
	"status":   domain.ActionStatus,
	"pause":    domain.ActionPause,
	"resume":   domain.ActionResume,
	"toggle":   domain.ActionToggle,
	"next":     domain.ActionNext,
	"previous": domain.ActionPrevious,
	"restart":  domain.ActionRestart,
}

func parsePlayerActions(spec string) ([]domain.TaskInput, error) {
	var inputs []domain.TaskInput
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		input, err := parsePlayerAction(token)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, domain.TaskInput{Kind: domain.PlayerControl, Player: &input})
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("-player-actions lists no actions")
	}
	return inputs, nil
}

func parsePlayerAction(token string) (domain.PlayerInput, error) {
	name, value, hasValue := strings.Cut(token, ":")
	if action, ok := simplePlayerActions[name]; ok {
		if hasValue {
			return domain.PlayerInput{}, fmt.Errorf("player action %q takes no value", name)
		}
		return domain.PlayerInput{Action: action}, nil
	}
	switch name {
	case "volume-up", "volume-down":
		if hasValue {
			return domain.PlayerInput{}, fmt.Errorf("player action %q takes no value", name)
		}
		step := 1
		if name == "volume-down" {
			step = -1
		}
		return domain.PlayerInput{Action: domain.ActionSetVolume, Volume: step, RelativeVolume: true}, nil
	case "volume", "volume-set":
		if !hasValue {
			return domain.PlayerInput{}, fmt.Errorf("player action %q needs a value, e.g. %s:-2", name, name)
		}
		number, err := wholeNumber(name, value)
		if err != nil {
			return domain.PlayerInput{}, err
		}
		return domain.PlayerInput{Action: domain.ActionSetVolume, Volume: number, RelativeVolume: name == "volume"}, nil
	}
	return parseNumberedAction(name, value, hasValue)
}

func parseNumberedAction(name, value string, hasValue bool) (domain.PlayerInput, error) {
	number, known := optionalNumberDefaults[name]
	if !known {
		return domain.PlayerInput{}, fmt.Errorf("unknown player action %q", name)
	}
	if hasValue {
		var err error
		if number, err = wholeNumber(name, value); err != nil {
			return domain.PlayerInput{}, err
		}
	}
	switch name {
	case "seek":
		return domain.PlayerInput{Action: domain.ActionSeek, PositionMs: number}, nil
	case "mode":
		return domain.PlayerInput{Action: domain.ActionSetPlayMode, PlayMode: number}, nil
	case "play-track":
		return domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: number}, nil
	default:
		return domain.PlayerInput{Action: domain.ActionLibrary, Limit: number}, nil
	}
}

func wholeNumber(name, value string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("player action %q needs a whole number, got %q", name, value)
	}
	return number, nil
}
