package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

type Remote interface {
	Control(domain.PlayerInput) (domain.PlayerResult, error)
	Cover() ([]byte, error)
	WalkLibrary(progress func(loaded, total int)) ([]domain.PlayerLibraryItem, error)
	WalkAlbums(progress func(loaded, total int)) ([]domain.PlayerAlbum, error)
	AlbumTracks(album string) ([]domain.PlayerLibraryItem, error)
}

type tickMsg time.Time

type statusMsg struct {
	status *domain.PlayerStatus
	err    error
}

type controlDoneMsg struct {
	label  string
	result domain.PlayerResult
	err    error
}

type albumTracksMsg struct {
	album  int
	tracks []domain.PlayerLibraryItem
	err    error
}

func tickEvery(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func pollStatus(remote Remote) tea.Cmd {
	return func() tea.Msg {
		result, err := remote.Control(domain.PlayerInput{Action: domain.ActionStatus})
		return statusMsg{status: result.Status, err: err}
	}
}

func runControl(remote Remote, label string, input domain.PlayerInput) tea.Cmd {
	return func() tea.Msg {
		result, err := remote.Control(input)
		return controlDoneMsg{label: label, result: result, err: err}
	}
}

func loadAlbumTracks(remote Remote, album int, name string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := remote.AlbumTracks(name)
		return albumTracksMsg{album: album, tracks: tracks, err: err}
	}
}
