package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

type listRow struct {
	number int
	text   string
}

func (m model) isListScreen() bool {
	return m.screen != nowPlayingScreen
}

func (m model) openAlbumTracks() []domain.PlayerLibraryItem {
	if m.openAlbum < 0 || m.openAlbum >= len(m.albums) {
		return nil
	}
	return m.albums[m.openAlbum].Tracks
}

func trackRows(tracks []domain.PlayerLibraryItem) []listRow {
	rows := make([]listRow, len(tracks))
	for index, track := range tracks {
		rows[index] = listRow{number: track.Position + 1, text: joinNonEmpty(" — ", track.Title, track.Artist)}
	}
	return rows
}

func (m model) rows() []listRow {
	switch m.screen {
	case tracksScreen:
		return trackRows(m.library)
	case albumTracksScreen:
		return trackRows(m.openAlbumTracks())
	case albumsScreen:
		rows := make([]listRow, len(m.albums))
		for index, album := range m.albums {
			rows[index] = listRow{number: album.Position + 1, text: fmt.Sprintf("%s · %d tracks", album.Name, album.TrackCount)}
		}
		return rows
	}
	return nil
}

func matchesQuery(text string, words []string) bool {
	lowered := strings.ToLower(text)
	for _, word := range words {
		if !strings.Contains(lowered, word) {
			return false
		}
	}
	return true
}

func (m model) visible() []int {
	rows := m.rows()
	words := strings.Fields(strings.ToLower(m.lists[m.screen].query))
	indexes := make([]int, 0, len(rows))
	for index, row := range rows {
		if len(words) == 0 || matchesQuery(row.text, words) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (m model) moveCursor(delta int) model {
	state := &m.lists[m.screen]
	state.cursor = max(0, min(len(m.visible())-1, state.cursor+delta))
	return m
}

func (m model) clampCursors() model {
	current := m.screen
	for list := tracksScreen; list < screenCount; list++ {
		m.screen = list
		m = m.moveCursor(0)
	}
	m.screen = current
	return m
}

func (m model) selectedIndex() (int, bool) {
	visible := m.visible()
	cursor := m.lists[m.screen].cursor
	if cursor < 0 || cursor >= len(visible) {
		return 0, false
	}
	return visible[cursor], true
}

func (m model) activateSelection() (tea.Model, tea.Cmd) {
	index, ok := m.selectedIndex()
	if !ok {
		return m, nil
	}
	switch m.screen {
	case tracksScreen:
		track := m.library[index]
		return m.playTrack(track, "")
	case albumsScreen:
		return m.openAlbumAt(index)
	case albumTracksScreen:
		track := m.openAlbumTracks()[index]
		return m.playTrack(track, m.albums[m.openAlbum].Name)
	}
	return m, nil
}

func (m model) playTrack(track domain.PlayerLibraryItem, album string) (tea.Model, tea.Cmd) {
	input := domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: track.Position, Locator: track.Locator, Album: album}
	return m.startControl(fmt.Sprintf("play %q", track.Title), input)
}

func (m model) openAlbumAt(index int) (tea.Model, tea.Cmd) {
	m.openAlbum, m.screen = index, albumTracksScreen
	m.lists[albumTracksScreen] = listState{}
	if m.albums[index].Tracks != nil || m.albumLoading {
		return m, nil
	}
	m.albumLoading = true
	return m, loadAlbumTracks(m.remote, index, m.albums[index].Name)
}

func (m model) closeAlbum() model {
	m.screen = albumsScreen
	return m
}

func (m model) setQuery(query string) model {
	m.lists[m.screen] = listState{query: query}
	return m
}
