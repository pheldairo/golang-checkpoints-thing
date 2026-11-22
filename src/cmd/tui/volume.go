package main

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

const (
	volumeRampStep = 5
	maxVolumeSteps = 60
)

func rampVolume(remote Remote, label string, target int) tea.Cmd {
	return func() tea.Msg {
		result, err := setVolumeGradually(remote, target)
		return controlDoneMsg{label: label, result: result, err: err}
	}
}

func setVolumeGradually(remote Remote, target int) (domain.PlayerResult, error) {
	status, err := remote.Control(domain.PlayerInput{Action: domain.ActionStatus})
	if err != nil {
		return status, err
	}
	if status.Status == nil {
		return status, fmt.Errorf("player status has no volume")
	}
	current := status.Status.Volume
	var result domain.PlayerResult
	for range maxVolumeSteps {
		next := target
		if target > current {
			next = min(target, current+volumeRampStep)
		}
		result, err = remote.Control(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: next})
		if err != nil {
			return result, err
		}
		current = result.Volume.After
		if current == target {
			return result, nil
		}
	}
	return result, fmt.Errorf("volume stopped at %d before reaching %d", current, target)
}

func (m model) setVolumeTo(target int) (tea.Model, tea.Cmd) {
	if m.status == nil {
		return m, nil
	}
	if target < 0 || target > m.status.MaxVolume {
		m.message, m.errorMsg = fmt.Sprintf("volume must be between 0 and %d", m.status.MaxVolume), true
		return m, nil
	}
	label := fmt.Sprintf("volume %d", target)
	if m.busy != "" {
		m.message, m.errorMsg = fmt.Sprintf("still waiting for %s", m.busy), true
		return m, nil
	}
	m.busy = label
	m.message, m.errorMsg = label+"…", false
	return m, rampVolume(m.remote, label, target)
}

func (m model) submitVolumeInput() (tea.Model, tea.Cmd) {
	text := m.inputText
	m.input, m.inputText = noInput, ""
	target, err := strconv.Atoi(text)
	if err != nil {
		m.message, m.errorMsg = fmt.Sprintf("%q is not a volume number", text), true
		return m, nil
	}
	return m.setVolumeTo(target)
}
