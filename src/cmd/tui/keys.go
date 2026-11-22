package main

import (
	"fmt"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

const (
	seekStepMs      = 10_000
	seekEndMarginMs = 5_000
)

func (m model) onKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.input != noInput {
		return m.onInputKey(msg)
	}
	if msg.Type != tea.KeyRunes || msg.Paste || len(msg.Runes) < 2 {
		return m.onKey(msg.String())
	}
	var cmds []tea.Cmd
	for _, key := range msg.Runes {
		next, cmd := m.onKey(string(key))
		m = next.(model)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) onInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.input == searchInput {
			m = m.setQuery("")
		}
		m.input, m.inputText = noInput, ""
		return m, nil
	case tea.KeyEnter:
		if m.input == volumeInput {
			return m.submitVolumeInput()
		}
		m.input = noInput
		return m, nil
	case tea.KeyBackspace:
		runes := []rune(m.inputText)
		if len(runes) > 0 {
			m.inputText = string(runes[:len(runes)-1])
		}
	case tea.KeySpace:
		m.inputText += " "
	case tea.KeyRunes:
		m.inputText += m.acceptedInput(msg.Runes)
	default:
		return m, nil
	}
	if m.input == searchInput {
		m = m.setQuery(m.inputText)
	}
	return m, nil
}

func (m model) acceptedInput(runes []rune) string {
	if m.input != volumeInput {
		return string(runes)
	}
	digits := []rune{}
	for _, character := range runes {
		if unicode.IsDigit(character) && len(m.inputText)+len(digits) < 3 {
			digits = append(digits, character)
		}
	}
	return string(digits)
}

func (m model) onKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "tab", "l":
		m.screen = nextTab(m.screen)
		return m, nil
	case "/":
		return m.startSearch(), nil
	case "v":
		if m.status == nil {
			return m, nil
		}
		m.input, m.inputText = volumeInput, ""
		return m, nil
	}
	if m.isListScreen() {
		if handled, next, cmd := m.onListKey(key); handled {
			return next, cmd
		}
	}
	input, label, ok := m.controlForKey(key)
	if !ok {
		return m, nil
	}
	return m.startControl(label, input)
}

func nextTab(current screen) screen {
	switch current {
	case nowPlayingScreen:
		return tracksScreen
	case tracksScreen:
		return albumsScreen
	}
	return nowPlayingScreen
}

func (m model) startSearch() model {
	if m.screen == nowPlayingScreen {
		m.screen = tracksScreen
	}
	m.input, m.inputText = searchInput, m.lists[m.screen].query
	return m
}

func (m model) onListKey(key string) (bool, tea.Model, tea.Cmd) {
	page := m.visibleListRows()
	switch key {
	case "up", "k":
		return true, m.moveCursor(-1), nil
	case "down", "j":
		return true, m.moveCursor(1), nil
	case "pgup", "ctrl+u":
		return true, m.moveCursor(-page), nil
	case "pgdown", "ctrl+d":
		return true, m.moveCursor(page), nil
	case "home", "g":
		return true, m.moveCursor(-len(m.rows())), nil
	case "end", "G":
		return true, m.moveCursor(len(m.rows())), nil
	case "R":
		next, cmd := m.startLibraryLoad(true)
		return true, next, cmd
	case "enter":
		next, cmd := m.activateSelection()
		return true, next, cmd
	case "esc", "backspace", "h":
		if m.lists[m.screen].query != "" {
			return true, m.setQuery(""), nil
		}
		if m.screen == albumTracksScreen {
			return true, m.closeAlbum(), nil
		}
	}
	return false, m, nil
}

func (m model) controlForKey(key string) (domain.PlayerInput, string, bool) {
	switch key {
	case " ", "space":
		return domain.PlayerInput{Action: domain.ActionToggle}, "play/pause", true
	case "n":
		return domain.PlayerInput{Action: domain.ActionNext}, "next", true
	case "p":
		return domain.PlayerInput{Action: domain.ActionPrevious}, "previous", true
	case "r":
		return domain.PlayerInput{Action: domain.ActionRestart}, "restart", true
	case "+", "=":
		return domain.PlayerInput{Action: domain.ActionSetVolume, Volume: 1, RelativeVolume: true}, "volume up", true
	case "-", "_":
		return domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -1, RelativeVolume: true}, "volume down", true
	case "m":
		return m.nextModeInput()
	case "right":
		return m.seekBy(seekStepMs)
	case "left":
		return m.seekBy(-seekStepMs)
	}
	return domain.PlayerInput{}, "", false
}

func (m model) nextModeInput() (domain.PlayerInput, string, bool) {
	if m.status == nil {
		return domain.PlayerInput{}, "", false
	}
	current, known := domain.PlayModeFromName(m.status.PlayMode)
	if !known {
		current = domain.PlayModeShuffle
	}
	next := domain.NextPlayMode(current)
	return domain.PlayerInput{Action: domain.ActionSetPlayMode, PlayMode: next}, "mode " + domain.PlayModeName(next), true
}

func (m model) seekBy(deltaMs int) (domain.PlayerInput, string, bool) {
	if m.status == nil || m.status.NowPlaying == nil {
		return domain.PlayerInput{}, "", false
	}
	return m.seekTo(m.status.ProgressMs + deltaMs)
}

func (m model) seekTo(targetMs int) (domain.PlayerInput, string, bool) {
	if m.status == nil || m.status.NowPlaying == nil {
		return domain.PlayerInput{}, "", false
	}
	target := max(0, targetMs)
	if duration := m.status.NowPlaying.DurationMs; duration > seekEndMarginMs {
		target = min(target, duration-seekEndMarginMs)
	}
	label := fmt.Sprintf("seek to %s", formatClock(target))
	return domain.PlayerInput{Action: domain.ActionSeek, PositionMs: target}, label, true
}
