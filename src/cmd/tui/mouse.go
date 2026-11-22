package main

import (
	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

const wheelRows = 3

func (m model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || m.input != noInput {
		return m, nil
	}
	f := m.frame()
	left, top := m.offsets(f)
	line, column := msg.Y-top, msg.X-left
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.onWheel(-1)
	case tea.MouseButtonWheelDown:
		return m.onWheel(1)
	case tea.MouseButtonLeft:
		return m.onClick(f, line, column)
	}
	return m, nil
}

func (m model) onWheel(direction int) (tea.Model, tea.Cmd) {
	if m.isListScreen() {
		return m.moveCursor(direction * wheelRows), nil
	}
	if m.status == nil {
		return m, nil
	}
	label := "volume up"
	if direction > 0 {
		label = "volume down"
	}
	return m.startControl(label, domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -direction, RelativeVolume: true})
}

func (m model) onClick(f frame, line, column int) (tea.Model, tea.Cmd) {
	for index, tab := range f.tabs {
		if tab.contains(line, column) {
			m.screen = tabScreens[index]
			return m, nil
		}
	}
	if m.isListScreen() && line >= f.listTop && line < f.listTop+f.listCount {
		return m.clickRow(f.listFirst + line - f.listTop)
	}
	if f.progress.contains(line, column) {
		return m.clickProgress(f.progress, column)
	}
	if f.volume.contains(line, column) {
		return m.clickVolume(f.volume, column)
	}
	return m, nil
}

func (m model) clickRow(position int) (tea.Model, tea.Cmd) {
	if position == m.lists[m.screen].cursor {
		return m.activateSelection()
	}
	m.lists[m.screen].cursor = position
	return m, nil
}

func (m model) clickProgress(bar region, column int) (tea.Model, tea.Cmd) {
	if m.status == nil || m.status.NowPlaying == nil {
		return m, nil
	}
	target := (column - bar.start) * m.status.NowPlaying.DurationMs / (bar.end - bar.start)
	input, label, ok := m.seekTo(target)
	if !ok {
		return m, nil
	}
	return m.startControl(label, input)
}

func (m model) clickVolume(bar region, column int) (tea.Model, tea.Cmd) {
	if m.status == nil {
		return m, nil
	}
	target := (column - bar.start + 1) * m.status.MaxVolume / (bar.end - bar.start)
	return m.setVolumeTo(target)
}
