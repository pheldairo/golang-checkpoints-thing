package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"setlist/internal/domain"
)

type stubRemote struct {
	lock       sync.Mutex
	inputs     []domain.PlayerInput
	status     domain.PlayerStatus
	library    []domain.PlayerLibraryItem
	albums     []domain.PlayerAlbum
	statusErr  error
	cover      []byte
	covers     int
	walks      int
	albumWalks int
	albumReads []string
}

func (s *stubRemote) Control(input domain.PlayerInput) (domain.PlayerResult, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.inputs = append(s.inputs, input)
	switch input.Action {
	case domain.ActionStatus:
		if s.statusErr != nil {
			return domain.PlayerResult{}, s.statusErr
		}
		status := s.status
		return domain.PlayerResult{Action: input.Action, Outcome: domain.OutcomeRead, Status: &status}, nil
	case domain.ActionSetVolume:
		before := s.status.Volume
		if input.RelativeVolume {
			s.status.Volume += input.Volume
		} else {
			if input.Volume-before > 5 {
				return domain.PlayerResult{}, errors.New("refused: more than 5 steps")
			}
			s.status.Volume = input.Volume
		}
		change := &domain.VolumeChange{Requested: s.status.Volume, Before: before, After: s.status.Volume, Maximum: s.status.MaxVolume}
		return domain.PlayerResult{Action: input.Action, Outcome: domain.OutcomeConfirmed, Volume: change}, nil
	}
	return domain.PlayerResult{Action: input.Action, Outcome: domain.OutcomeConfirmed}, nil
}

func (s *stubRemote) Cover() ([]byte, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.covers++
	return s.cover, nil
}

func (s *stubRemote) WalkLibrary(progress func(loaded, total int)) ([]domain.PlayerLibraryItem, error) {
	s.lock.Lock()
	s.walks++
	tracks := append([]domain.PlayerLibraryItem(nil), s.library...)
	s.lock.Unlock()
	for loaded := 50; loaded < len(tracks); loaded += 50 {
		progress(loaded, len(tracks))
	}
	progress(len(tracks), len(tracks))
	return tracks, nil
}

func (s *stubRemote) WalkAlbums(progress func(loaded, total int)) ([]domain.PlayerAlbum, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.albumWalks++
	progress(len(s.albums), len(s.albums))
	return append([]domain.PlayerAlbum(nil), s.albums...), nil
}

func (s *stubRemote) AlbumTracks(album string) ([]domain.PlayerLibraryItem, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.albumReads = append(s.albumReads, album)
	var tracks []domain.PlayerLibraryItem
	for position, track := range s.library[:3] {
		track.Position = position
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (s *stubRemote) sentSince(count int) []domain.PlayerInput {
	s.lock.Lock()
	defer s.lock.Unlock()
	return append([]domain.PlayerInput(nil), s.inputs[count:]...)
}

func (s *stubRemote) actions(inputs []domain.PlayerInput, action domain.PlayerAction) []domain.PlayerInput {
	var matching []domain.PlayerInput
	for _, input := range inputs {
		if input.Action == action {
			matching = append(matching, input)
		}
	}
	return matching
}

func playingRemote() *stubRemote {
	var library []domain.PlayerLibraryItem
	for position := range 130 {
		library = append(library, domain.PlayerLibraryItem{
			Position: position,
			Title:    fmt.Sprintf("Song %d", position+1),
			Artist:   []string{"Band", "Choir", "Duo"}[position%3],
			Locator:  fmt.Sprintf(`<path>a:\%d.flac<cue_id>-1<name>Song %d`, position+1, position+1),
		})
	}
	return &stubRemote{
		status: domain.PlayerStatus{
			Model: "hm001", Volume: 40, MaxVolume: 100, PlayState: "playing", PlayMode: "shuffle", ProgressMs: 61000,
			NowPlaying: &domain.PlayerTrack{Title: "Blue Hour", Artist: "Demo Trio", Album: "After Hours", Path: `\a\1.flac`, DurationMs: 240000, Format: "FLAC"},
		},
		library: library,
		albums: []domain.PlayerAlbum{
			{Position: 0, Name: "Abbey Road", TrackCount: 3},
			{Position: 1, Name: "Kid A", TrackCount: 3},
		},
	}
}

func execute(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, inner := range batch {
			messages = append(messages, execute(inner)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func drive(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			t.Fatal("commands kept producing more commands")
		}
		current := queue[0]
		queue = queue[1:]
		for _, message := range execute(current) {
			if _, isTick := message.(tickMsg); isTick || message == nil {
				continue
			}
			next, more := m.Update(message)
			m = next.(model)
			if more != nil {
				queue = append(queue, more)
			}
		}
	}
	return m
}

func send(t *testing.T, m model, message tea.Msg) model {
	t.Helper()
	next, cmd := m.Update(message)
	return drive(t, next.(model), cmd)
}

func runeKey(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

func key(keyType tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: keyType}
}

func newTestModel(t *testing.T, remote *stubRemote, cachePath string) model {
	t.Helper()
	m := newModel(remote, "127.0.0.1", cachePath, time.Millisecond)
	m.coverSettle = 0
	m = drive(t, m, m.Init())
	return send(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
}

func startedModel(t *testing.T, remote *stubRemote) model {
	t.Helper()
	return newTestModel(t, remote, filepath.Join(t.TempDir(), "library.json"))
}

func screenLine(m model, line int) string {
	return ansi.Strip(strings.Split(m.View(), "\n")[line])
}

func click(t *testing.T, m model, area region) model {
	t.Helper()
	left, top := m.offsets(m.frame())
	return send(t, m, tea.MouseMsg{X: left + area.start, Y: top + area.line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func wheel(t *testing.T, m model, button tea.MouseButton) model {
	t.Helper()
	return send(t, m, tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: button})
}

func coverImage(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			shade := color.RGBA{R: 255, A: 255}
			if y >= 4 {
				shade = color.RGBA{B: 255, A: 255}
			}
			picture.Set(x, y, shade)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestStartupReadsStatusAndShowsNowPlaying(t *testing.T) {
	view := ansi.Strip(startedModel(t, playingRemote()).View())
	for _, text := range []string{"Now playing", "Tracks", "Albums", "Blue Hour", "Demo Trio", "After Hours · FLAC", "▶ Playing", "01:01", "04:00", "40/100", "shuffle"} {
		if !strings.Contains(view, text) {
			t.Errorf("view is missing %q:\n%s", text, view)
		}
	}
}

func TestStatusErrorIsShownAsDisconnected(t *testing.T) {
	remote := playingRemote()
	remote.statusErr = errors.New("connection refused")
	view := ansi.Strip(startedModel(t, remote).View())
	if !strings.Contains(view, "disconnected") || !strings.Contains(view, "connection refused") {
		t.Fatalf("error not shown:\n%s", view)
	}
}

func TestInterfaceIsCenteredInTheTerminal(t *testing.T) {
	m := send(t, startedModel(t, playingRemote()), tea.WindowSizeMsg{Width: 200, Height: 50})
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if len(lines) != 50 || strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("view has %d lines, first %q", len(lines), lines[0])
	}
	f := m.frame()
	left, top := m.offsets(f)
	if left != (200-f.width())/2 || top != (50-len(f.lines))/2 || left < 40 {
		t.Fatalf("offsets %d,%d for a %dx%d block", left, top, f.width(), len(f.lines))
	}
}

func TestKeysSendTheMatchingControls(t *testing.T) {
	cases := []struct {
		key  tea.KeyMsg
		want domain.PlayerInput
	}{
		{key(tea.KeySpace), domain.PlayerInput{Action: domain.ActionToggle}},
		{runeKey("n"), domain.PlayerInput{Action: domain.ActionNext}},
		{runeKey("p"), domain.PlayerInput{Action: domain.ActionPrevious}},
		{runeKey("r"), domain.PlayerInput{Action: domain.ActionRestart}},
		{runeKey("+"), domain.PlayerInput{Action: domain.ActionSetVolume, Volume: 1, RelativeVolume: true}},
		{runeKey("-"), domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -1, RelativeVolume: true}},
		{runeKey("m"), domain.PlayerInput{Action: domain.ActionSetPlayMode, PlayMode: domain.PlayModeInOrder}},
		{key(tea.KeyRight), domain.PlayerInput{Action: domain.ActionSeek, PositionMs: 71000}},
		{key(tea.KeyLeft), domain.PlayerInput{Action: domain.ActionSeek, PositionMs: 51000}},
	}
	for _, testCase := range cases {
		remote := playingRemote()
		m := startedModel(t, remote)
		before := len(remote.inputs)
		m = send(t, m, testCase.key)
		if got := remote.sentSince(before)[0]; got != testCase.want {
			t.Errorf("%s sent %+v, want %+v", testCase.key, got, testCase.want)
		}
		if m.busy != "" || m.errorMsg {
			t.Errorf("%s left busy=%q message=%q", testCase.key, m.busy, m.message)
		}
	}
}

func TestSeekIsClampedBeforeTheTrackEnd(t *testing.T) {
	remote := playingRemote()
	remote.status.ProgressMs = 238000
	m := startedModel(t, remote)
	before := len(remote.inputs)
	send(t, m, key(tea.KeyRight))
	if got := remote.sentSince(before)[0].PositionMs; got != 235000 {
		t.Fatalf("seek target %d, want 235000", got)
	}
}

func TestOnlyOneControlRunsAtATime(t *testing.T) {
	m := startedModel(t, playingRemote())
	next, first := m.Update(runeKey("n"))
	next, second := next.(model).Update(runeKey("p"))
	m = next.(model)
	if first == nil || second != nil || !strings.Contains(m.message, "still waiting for next") {
		t.Fatalf("second control was not held back: %q", m.message)
	}
	next, cmd := m.Update(tickMsg(time.Now()))
	if messages := execute(cmd); len(messages) != 1 || next.(model).polling {
		t.Fatal("status was polled while a control was running")
	}
}

func TestFailedControlIsReported(t *testing.T) {
	m := startedModel(t, playingRemote())
	next, cmd := m.Update(controlDoneMsg{label: "next", err: errors.New("player did not confirm")})
	m = next.(model)
	if !m.errorMsg || !strings.Contains(ansi.Strip(m.View()), "next failed: player did not confirm") || cmd == nil {
		t.Fatalf("failure not reported: %q", m.message)
	}
}

func TestLibraryAndAlbumsAreReadOnceThenLoadedFromTheSavedFile(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "library.json")
	remote := playingRemote()
	first := newTestModel(t, remote, cachePath)
	if remote.walks != 1 || remote.albumWalks != 1 || len(first.library) != 130 || len(first.albums) != 2 {
		t.Fatalf("first start: %d walks, %d album walks, %d tracks", remote.walks, remote.albumWalks, len(first.library))
	}
	if !strings.Contains(first.message, "130 tracks, 2 albums saved") {
		t.Fatalf("save not reported: %q", first.message)
	}
	second := newTestModel(t, remote, cachePath)
	if remote.walks != 1 || remote.albumWalks != 1 || len(second.library) != 130 || second.library[129].Locator == "" {
		t.Fatalf("second start read the player again: %d walks", remote.walks)
	}
	send(t, send(t, second, key(tea.KeyTab)), runeKey("R"))
	if remote.walks != 2 || remote.albumWalks != 2 {
		t.Fatalf("R did not reload, %d walks", remote.walks)
	}
}

func TestSavedLibraryWithoutAlbumsReadsOnlyTheAlbums(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "library.json")
	remote := playingRemote()
	if err := saveLibraryCache(cachePath, libraryCacheFile{Tracks: remote.library}); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, remote, cachePath)
	if remote.walks != 0 || remote.albumWalks != 1 || len(m.albums) != 2 {
		t.Fatalf("%d track walks, %d album walks", remote.walks, remote.albumWalks)
	}
}

func TestLibraryProgressIsShownWhileLoading(t *testing.T) {
	m := newModel(playingRemote(), "127.0.0.1", filepath.Join(t.TempDir(), "x.json"), time.Second)
	m.screen, m.libraryLoading = tracksScreen, true
	next, _ := m.Update(libraryProgressMsg{phase: phaseTracks, loaded: 1200, total: 4270})
	view := ansi.Strip(next.(model).View())
	if !strings.Contains(view, "Reading tracks from the player") || !strings.Contains(view, "1200/4270 · 28%") {
		t.Fatalf("progress not shown:\n%s", view)
	}
}

func TestTrackNavigationAndPlayByLocator(t *testing.T) {
	remote := playingRemote()
	m := send(t, startedModel(t, remote), key(tea.KeyTab))
	m = send(t, m, runeKey("jjj"))
	if m.lists[tracksScreen].cursor != 3 {
		t.Fatalf("cursor %d after three j presses", m.lists[tracksScreen].cursor)
	}
	m = send(t, send(t, m, runeKey("G")), runeKey("g"))
	m = send(t, m, key(tea.KeyPgDown))
	if m.lists[tracksScreen].cursor != m.visibleListRows() {
		t.Fatalf("page down moved to %d", m.lists[tracksScreen].cursor)
	}
	before := len(remote.inputs)
	m = send(t, m, key(tea.KeyEnter))
	played := remote.sentSince(before)[0]
	wanted := m.library[m.lists[tracksScreen].cursor]
	if played.Action != domain.ActionPlayTrack || played.Locator != wanted.Locator || played.TrackIndex != wanted.Position || played.Album != "" {
		t.Fatalf("enter sent %+v", played)
	}
}

func TestSearchFiltersTheListAndPlaysTheMatch(t *testing.T) {
	remote := playingRemote()
	m := send(t, startedModel(t, remote), runeKey("/"))
	if m.screen != tracksScreen || m.input != searchInput {
		t.Fatal("/ did not open search on the track list")
	}
	m = send(t, m, runeKey("song 12"))
	visible := m.visible()
	if len(visible) != 12 || m.library[visible[0]].Title != "Song 12" {
		t.Fatalf("%d matches for 'song 12'", len(visible))
	}
	if !strings.Contains(ansi.Strip(m.View()), "/song 12▌") {
		t.Fatal("search text not shown")
	}
	m = send(t, m, key(tea.KeySpace))
	m = send(t, m, runeKey("duo"))
	if len(m.visible()) != 5 {
		t.Fatalf("%d matches for 'song 12 duo'", len(m.visible()))
	}
	m = send(t, m, key(tea.KeyEnter))
	if m.input != noInput || m.lists[tracksScreen].query != "song 12 duo" {
		t.Fatal("enter did not keep the filter")
	}
	before := len(remote.inputs)
	m = send(t, send(t, m, runeKey("j")), key(tea.KeyEnter))
	played := remote.sentSince(before)[0]
	if played.Locator != m.library[m.visible()[1]].Locator {
		t.Fatalf("played %+v, want the second match", played)
	}
	m = send(t, m, key(tea.KeyEsc))
	if m.lists[tracksScreen].query != "" || len(m.visible()) != 130 {
		t.Fatal("esc did not clear the filter")
	}
}

func TestAlbumsOpenLoadOnceAndPlayInAlbumContext(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "library.json")
	remote := playingRemote()
	m := newTestModel(t, remote, cachePath)
	m = send(t, send(t, m, key(tea.KeyTab)), key(tea.KeyTab))
	if m.screen != albumsScreen || !strings.Contains(ansi.Strip(m.View()), "Abbey Road · 3 tracks") {
		t.Fatalf("albums screen not shown:\n%s", ansi.Strip(m.View()))
	}
	m = send(t, m, key(tea.KeyEnter))
	if m.screen != albumTracksScreen || len(m.openAlbumTracks()) != 3 || len(remote.albumReads) != 1 {
		t.Fatalf("album not opened: screen %d, %d tracks", m.screen, len(m.openAlbumTracks()))
	}
	before := len(remote.inputs)
	m = send(t, m, key(tea.KeyEnter))
	played := remote.sentSince(before)[0]
	if played.Album != "Abbey Road" || played.Locator == "" {
		t.Fatalf("album play sent %+v", played)
	}
	m = send(t, m, key(tea.KeyEsc))
	if m.screen != albumsScreen {
		t.Fatal("esc did not go back to the albums")
	}
	send(t, m, key(tea.KeyEnter))
	if len(remote.albumReads) != 1 {
		t.Fatal("album tracks were read from the player twice")
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var saved libraryCacheFile
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Albums[0].Tracks) != 3 {
		t.Fatalf("album tracks not saved: %v", err)
	}
}

func TestVolumeCanBeTypedAndRampsUpInSafeSteps(t *testing.T) {
	remote := playingRemote()
	m := send(t, startedModel(t, remote), runeKey("v"))
	if m.input != volumeInput || !strings.Contains(ansi.Strip(m.View()), "volume (0–100): ▌") {
		t.Fatal("v did not open volume entry")
	}
	before := len(remote.inputs)
	m = send(t, send(t, m, runeKey("5x7")), key(tea.KeyEnter))
	steps := remote.actions(remote.sentSince(before), domain.ActionSetVolume)
	var targets []int
	for _, step := range steps {
		targets = append(targets, step.Volume)
	}
	if fmt.Sprint(targets) != "[45 50 55 57]" || remote.status.Volume != 57 || m.errorMsg {
		t.Fatalf("ramp sent %v, volume %d, message %q", targets, remote.status.Volume, m.message)
	}
	before = len(remote.inputs)
	m = send(t, send(t, send(t, m, runeKey("v")), runeKey("12")), key(tea.KeyEnter))
	if targets := remote.actions(remote.sentSince(before), domain.ActionSetVolume); len(targets) != 1 || targets[0].Volume != 12 {
		t.Fatalf("lowering sent %+v", targets)
	}
	m = send(t, send(t, send(t, m, runeKey("v")), runeKey("150")), key(tea.KeyEnter))
	if !m.errorMsg || !strings.Contains(m.message, "between 0 and 100") {
		t.Fatalf("out of range volume accepted: %q", m.message)
	}
	m = send(t, send(t, m, runeKey("v")), key(tea.KeyEsc))
	if m.input != noInput {
		t.Fatal("esc did not cancel volume entry")
	}
}

func TestMouseTabsRowsAndWheel(t *testing.T) {
	remote := playingRemote()
	m := startedModel(t, remote)
	f := m.frame()
	_, top := m.offsets(f)
	header := []rune(screenLine(m, top))
	left, _ := m.offsets(f)
	if label := string(header[left+f.tabs[1].start : left+f.tabs[1].end]); label != "Tracks" {
		t.Fatalf("tab region points at %q", label)
	}
	m = click(t, m, f.tabs[1])
	if m.screen != tracksScreen {
		t.Fatal("clicking the Tracks tab did not switch")
	}
	m = wheel(t, m, tea.MouseButtonWheelDown)
	if m.lists[tracksScreen].cursor != wheelRows {
		t.Fatalf("wheel moved the cursor to %d", m.lists[tracksScreen].cursor)
	}
	f = m.frame()
	_, top = m.offsets(f)
	row := region{line: f.listTop + 5, start: 4, end: 5, valid: true}
	m = click(t, m, row)
	selected := f.listFirst + 5
	if m.lists[tracksScreen].cursor != selected {
		t.Fatalf("click selected %d, want %d", m.lists[tracksScreen].cursor, selected)
	}
	if !strings.Contains(screenLine(m, top+row.line), m.library[selected].Title) {
		t.Fatalf("clicked line does not show the selected track")
	}
	before := len(remote.inputs)
	m = click(t, m, row)
	if played := remote.sentSince(before)[0]; played.Locator != m.library[selected].Locator {
		t.Fatalf("second click played %+v", played)
	}
	m = click(t, m, m.frame().tabs[0])
	before = len(remote.inputs)
	m = wheel(t, m, tea.MouseButtonWheelUp)
	if volume := remote.sentSince(before)[0]; volume.Volume != 1 || !volume.RelativeVolume {
		t.Fatalf("wheel up on now playing sent %+v", volume)
	}
}

func TestMouseOnProgressAndVolumeBars(t *testing.T) {
	remote := playingRemote()
	m := startedModel(t, remote)
	f := m.frame()
	_, top := m.offsets(f)
	left, _ := m.offsets(f)
	progressText := []rune(screenLine(m, top+f.progress.line))
	if string(progressText[left+f.progress.start-1]) != " " || !strings.ContainsRune("━─", progressText[left+f.progress.start]) {
		t.Fatalf("progress region does not start at the bar: %q", string(progressText))
	}
	middle := f.progress
	middle.start = (f.progress.start + f.progress.end) / 2
	before := len(remote.inputs)
	m = click(t, m, middle)
	seek := remote.sentSince(before)[0]
	if seek.Action != domain.ActionSeek || seek.PositionMs < 110000 || seek.PositionMs > 130000 {
		t.Fatalf("clicking the middle of the bar sent %+v", seek)
	}
	volumeAt := f.volume
	volumeAt.start = f.volume.start + volumeBarWidth/2 - 1
	before = len(remote.inputs)
	click(t, m, volumeAt)
	steps := remote.actions(remote.sentSince(before), domain.ActionSetVolume)
	if len(steps) != 2 || steps[1].Volume != 50 {
		t.Fatalf("clicking the middle of the volume bar sent %+v", steps)
	}
}

func TestCoverIsFetchedAndDrawn(t *testing.T) {
	remote := playingRemote()
	remote.cover = coverImage(t)
	m := startedModel(t, remote)
	if m.coverArt == "" || remote.covers != 1 {
		t.Fatalf("cover not drawn after %d fetches", remote.covers)
	}
	if strings.Count(m.coverArt, upperHalfCell) != coverColumns*coverRows {
		t.Fatalf("cover has %d cells", strings.Count(m.coverArt, upperHalfCell))
	}
	drive(t, m, pollStatus(remote))
	if remote.covers != 1 {
		t.Fatal("cover fetched again for the same track")
	}
}

func TestMissingCoverIsRetriedThenShownAsNoCover(t *testing.T) {
	remote := playingRemote()
	m := startedModel(t, remote)
	for tick := 0; tick < 20 && m.coverNote == ""; tick++ {
		m = send(t, m, tickMsg(time.Now()))
	}
	if m.coverNote != "no cover" || remote.covers != coverRetries {
		t.Fatalf("note %q after %d fetches", m.coverNote, remote.covers)
	}
}

func TestCoverIsFlippedBecauseThePlayerStoresRowsBottomUp(t *testing.T) {
	decoded, _, err := image.Decode(bytes.NewReader(coverImage(t)))
	if err != nil {
		t.Fatal(err)
	}
	pixels := coverPixels(decoded, 2, 2)
	if pixels[0][0].B != 255 || pixels[1][0].R != 255 {
		t.Fatalf("top row %v, bottom row %v: image was not flipped", pixels[0][0], pixels[1][0])
	}
	if _, err := renderCover([]byte("\xff\xd8synthetic-jpeg-Title\xff\xd9"), coverColumns, coverRows); err == nil {
		t.Fatal("a fake JPEG was decoded")
	}
}

func TestQuitKeys(t *testing.T) {
	m := startedModel(t, playingRemote())
	for _, quitKey := range []tea.KeyMsg{runeKey("q"), key(tea.KeyCtrlC)} {
		_, cmd := m.Update(quitKey)
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Errorf("%s did not quit", quitKey)
		}
	}
	searching := send(t, m, runeKey("/"))
	next, _ := searching.Update(runeKey("q"))
	if next.(model).inputText != "q" {
		t.Fatal("q quit while typing a search")
	}
}

func TestFormatting(t *testing.T) {
	if formatClock(61000) != "01:01" || formatClock(3_725_000) != "1:02:05" || formatClock(-5) != "00:00" {
		t.Fatal("clock formatting")
	}
	if truncate("abcdefghij", 5) != "abcd…" || truncate("abc", 5) != "abc" {
		t.Fatal("truncation")
	}
}
