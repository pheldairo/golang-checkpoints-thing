package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/domain"
)

type screen int

const (
	nowPlayingScreen screen = iota
	tracksScreen
	albumsScreen
	albumTracksScreen
	screenCount
)

type inputMode int

const (
	noInput inputMode = iota
	searchInput
	volumeInput
)

type listState struct {
	cursor int
	query  string
}

type model struct {
	remote    Remote
	host      string
	refresh   time.Duration
	cachePath string

	status    *domain.PlayerStatus
	statusErr error
	polling   bool

	busy     string
	message  string
	errorMsg bool

	screen    screen
	lists     [screenCount]listState
	input     inputMode
	inputText string

	library        []domain.PlayerLibraryItem
	albums         []domain.PlayerAlbum
	openAlbum      int
	albumLoading   bool
	librarySavedAt time.Time
	libraryLoading bool
	loadPhase      string
	libraryLoaded  int
	libraryTotal   int
	libraryUpdates <-chan tea.Msg

	trackPath     string
	trackSeenAt   time.Time
	coverPath     string
	coverArt      string
	coverNote     string
	coverLoading  bool
	coverAttempts int
	coverSettle   time.Duration

	width  int
	height int
}

func newModel(remote Remote, host, cachePath string, refresh time.Duration) model {
	return model{
		remote:      remote,
		host:        host,
		refresh:     refresh,
		cachePath:   cachePath,
		polling:     true,
		openAlbum:   -1,
		coverSettle: coverSettle,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(pollStatus(m.remote), tickEvery(m.refresh), readLibraryCache(m.cachePath))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m.onTick()
	case statusMsg:
		return m.onStatus(msg)
	case coverMsg:
		return m.onCover(msg)
	case controlDoneMsg:
		return m.onControlDone(msg)
	case libraryCacheMsg:
		return m.onLibraryCache(msg)
	case libraryProgressMsg:
		m.loadPhase, m.libraryLoaded, m.libraryTotal = msg.phase, msg.loaded, msg.total
		return m, waitForLibrary(m.libraryUpdates)
	case libraryLoadedMsg:
		return m.onLibraryLoaded(msg)
	case librarySavedMsg:
		return m.onLibrarySaved(msg)
	case albumTracksMsg:
		return m.onAlbumTracks(msg)
	case tea.KeyMsg:
		return m.onKeyMsg(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	}
	return m, nil
}

func (m model) deviceBusy() bool {
	return m.polling || m.busy != "" || m.coverLoading
}

func (m model) onTick() (tea.Model, tea.Cmd) {
	next := tickEvery(m.refresh)
	if m.deviceBusy() {
		return m, next
	}
	if m.coverDue() {
		return m.startCoverFetch(next)
	}
	m.polling = true
	return m, tea.Batch(pollStatus(m.remote), next)
}

func (m model) onStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	m.polling = false
	m.status, m.statusErr = msg.status, msg.err
	path := ""
	if m.status != nil && m.status.NowPlaying != nil {
		path = m.status.NowPlaying.Path
	}
	if path != m.trackPath {
		m.trackPath, m.trackSeenAt = path, time.Now()
		m.coverArt, m.coverNote, m.coverAttempts = "", "", 0
	}
	if !m.deviceBusy() && m.coverDue() {
		return m.startCoverFetch(nil)
	}
	return m, nil
}

func (m model) coverDue() bool {
	return m.trackPath != "" && m.coverPath != m.trackPath && time.Since(m.trackSeenAt) >= m.coverSettle
}

func (m model) startCoverFetch(next tea.Cmd) (tea.Model, tea.Cmd) {
	m.coverLoading = true
	return m, tea.Batch(fetchCover(m.remote, m.trackPath), next)
}

func (m model) onCover(msg coverMsg) (tea.Model, tea.Cmd) {
	m.coverLoading = false
	if msg.path != m.trackPath {
		return m, nil
	}
	switch {
	case msg.err != nil:
		m.coverPath, m.coverNote = msg.path, "cover unavailable"
	case msg.art != "":
		m.coverPath, m.coverArt = msg.path, msg.art
	case msg.note != "":
		m.coverPath, m.coverNote = msg.path, msg.note
	default:
		m.coverAttempts++
		m.trackSeenAt = time.Now()
		if m.coverAttempts >= coverRetries {
			m.coverPath, m.coverNote = msg.path, "no cover"
		}
	}
	return m, nil
}

func (m model) onControlDone(msg controlDoneMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.err != nil {
		m.message, m.errorMsg = fmt.Sprintf("%s failed: %v", msg.label, msg.err), true
	} else {
		m.message, m.errorMsg = fmt.Sprintf("%s: %s", msg.label, msg.result.Outcome), false
	}
	if m.polling {
		return m, nil
	}
	m.polling = true
	return m, pollStatus(m.remote)
}

func (m model) startControl(label string, input domain.PlayerInput) (tea.Model, tea.Cmd) {
	if m.busy != "" {
		m.message, m.errorMsg = fmt.Sprintf("still waiting for %s", m.busy), true
		return m, nil
	}
	m.busy = label
	m.message, m.errorMsg = label+"…", false
	return m, runControl(m.remote, label, input)
}

func (m model) onLibraryCache(msg libraryCacheMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if !isMissingFile(msg.err) {
			m.message, m.errorMsg = msg.err.Error(), true
		}
		return m.startLibraryLoad(true)
	}
	m.library, m.albums, m.librarySavedAt = msg.cache.Tracks, msg.cache.Albums, msg.cache.SavedAt
	if m.albums == nil {
		return m.startLibraryLoad(false)
	}
	return m, nil
}

func (m model) startLibraryLoad(includeTracks bool) (tea.Model, tea.Cmd) {
	if m.libraryLoading {
		return m, nil
	}
	m.libraryLoading, m.loadPhase, m.libraryLoaded, m.libraryTotal = true, phaseTracks, 0, 0
	if !includeTracks {
		m.loadPhase = phaseAlbums
	}
	updates, wait := startLibraryWalk(m.remote, includeTracks)
	m.libraryUpdates = updates
	return m, wait
}

func (m model) onLibraryLoaded(msg libraryLoadedMsg) (tea.Model, tea.Cmd) {
	m.libraryLoading = false
	if msg.err != nil {
		m.message, m.errorMsg = fmt.Sprintf("library failed: %v", msg.err), true
		return m, nil
	}
	if msg.tracksIncluded {
		m.library = msg.tracks
	}
	m.albums, m.openAlbum, m.librarySavedAt = msg.albums, -1, time.Now()
	if m.screen == albumTracksScreen {
		m.screen = albumsScreen
	}
	m = m.clampCursors()
	return m, m.saveLibrary()
}

func (m model) saveLibrary() tea.Cmd {
	cache := libraryCacheFile{Host: m.host, SavedAt: m.librarySavedAt, Tracks: m.library, Albums: m.albums}
	return writeLibraryCache(m.cachePath, cache)
}

func (m model) onLibrarySaved(msg librarySavedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.message, m.errorMsg = fmt.Sprintf("could not save the library: %v", msg.err), true
	} else {
		m.message, m.errorMsg = fmt.Sprintf("library: %d tracks, %d albums saved to %s", msg.count, len(m.albums), msg.path), false
	}
	return m, nil
}

func (m model) onAlbumTracks(msg albumTracksMsg) (tea.Model, tea.Cmd) {
	m.albumLoading = false
	if msg.album < 0 || msg.album >= len(m.albums) {
		return m, nil
	}
	if msg.err != nil {
		m.message, m.errorMsg = fmt.Sprintf("album failed: %v", msg.err), true
		return m, nil
	}
	albums := append([]domain.PlayerAlbum(nil), m.albums...)
	albums[msg.album].Tracks = msg.tracks
	m.albums = albums
	return m, m.saveLibrary()
}
