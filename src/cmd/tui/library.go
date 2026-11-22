package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

const (
	phaseTracks = "tracks"
	phaseAlbums = "albums"
)

type libraryCacheFile struct {
	Host    string
	SavedAt time.Time
	Tracks  []domain.PlayerLibraryItem
	Albums  []domain.PlayerAlbum
}

type libraryCacheMsg struct {
	cache libraryCacheFile
	err   error
}

type libraryProgressMsg struct {
	phase  string
	loaded int
	total  int
}

type libraryLoadedMsg struct {
	tracks         []domain.PlayerLibraryItem
	albums         []domain.PlayerAlbum
	tracksIncluded bool
	err            error
}

type librarySavedMsg struct {
	path  string
	count int
	err   error
}

func defaultCachePath(host string) string {
	return fmt.Sprintf("player-library-%s.json", host)
}

func readLibraryCache(path string) tea.Cmd {
	return func() tea.Msg {
		raw, err := os.ReadFile(path)
		if err != nil {
			return libraryCacheMsg{err: err}
		}
		var cache libraryCacheFile
		if err := json.Unmarshal(raw, &cache); err != nil {
			return libraryCacheMsg{err: fmt.Errorf("saved library %s is unreadable: %w", path, err)}
		}
		return libraryCacheMsg{cache: cache}
	}
}

func writeLibraryCache(path string, cache libraryCacheFile) tea.Cmd {
	return func() tea.Msg {
		err := saveLibraryCache(path, cache)
		return librarySavedMsg{path: path, count: len(cache.Tracks), err: err}
	}
}

func saveLibraryCache(path string, cache libraryCacheFile) error {
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".player-library-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func isMissingFile(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func startLibraryWalk(remote Remote, includeTracks bool) (<-chan tea.Msg, tea.Cmd) {
	updates := make(chan tea.Msg, 64)
	go func() {
		defer close(updates)
		updates <- walkPlayerLibrary(remote, includeTracks, func(phase string, loaded, total int) {
			updates <- libraryProgressMsg{phase: phase, loaded: loaded, total: total}
		})
	}()
	return updates, waitForLibrary(updates)
}

func walkPlayerLibrary(remote Remote, includeTracks bool, progress func(phase string, loaded, total int)) libraryLoadedMsg {
	result := libraryLoadedMsg{tracksIncluded: includeTracks}
	if includeTracks {
		result.tracks, result.err = remote.WalkLibrary(func(loaded, total int) { progress(phaseTracks, loaded, total) })
		if result.err != nil {
			return result
		}
	}
	result.albums, result.err = remote.WalkAlbums(func(loaded, total int) { progress(phaseAlbums, loaded, total) })
	return result
}

func waitForLibrary(updates <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		message, open := <-updates
		if !open {
			return nil
		}
		return message
	}
}
