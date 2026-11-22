package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	volumeBarWidth   = 16
	coverGap         = 3
	minimumInfoWidth = 30
	progressLineSlot = 6
	volumeLineSlot   = 8
	infoLineCount    = 10
	volumeLabel      = "volume "
)

var (
	accent           = lipgloss.AdaptiveColor{Light: "#1f6feb", Dark: "#58a6ff"}
	muted            = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#8b949e"}
	failure          = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
	placeholderShade = lipgloss.AdaptiveColor{Light: "#eaeef2", Dark: "#21262d"}
	titleStyle       = lipgloss.NewStyle().Bold(true)
	mutedStyle       = lipgloss.NewStyle().Foreground(muted)
	accentStyle      = lipgloss.NewStyle().Foreground(accent)
	errorStyle       = lipgloss.NewStyle().Foreground(failure)
	panelStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
	cursorStyle      = lipgloss.NewStyle().Foreground(accent).Bold(true)
	activeTabStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true).Underline(true)
	tabScreens       = []screen{nowPlayingScreen, tracksScreen, albumsScreen}
	tabLabels        = []string{"Now playing", "Tracks", "Albums"}
)

func spread(left, right string, width int) string {
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m model) activeTab() screen {
	if m.screen == albumTracksScreen {
		return albumsScreen
	}
	return m.screen
}

func (m model) header(width int) (string, []region) {
	var left strings.Builder
	regions := make([]region, len(tabLabels))
	for index, label := range tabLabels {
		if index > 0 {
			left.WriteString(mutedStyle.Render(" │ "))
		}
		start := lipgloss.Width(left.String())
		if tabScreens[index] == m.activeTab() {
			left.WriteString(activeTabStyle.Render(label))
		} else {
			left.WriteString(mutedStyle.Render(label))
		}
		regions[index] = region{line: 0, start: start, end: start + lipgloss.Width(label), valid: true}
	}
	right := mutedStyle.Render(m.host+" ") + accentStyle.Render("●")
	if m.statusErr != nil {
		right = mutedStyle.Render(m.host+" ") + errorStyle.Render("● disconnected")
	} else if m.status == nil {
		right = mutedStyle.Render(m.host + " ○")
	}
	return spread(left.String(), right, width), regions
}

func (m model) footer(width int) string {
	switch m.input {
	case searchInput:
		count := len(m.visible())
		return truncate(fmt.Sprintf("/%s▌", m.inputText)+mutedStyle.Render(fmt.Sprintf("  %d matches · enter keep · esc clear", count)), width)
	case volumeInput:
		maximum := 0
		if m.status != nil {
			maximum = m.status.MaxVolume
		}
		return truncate(fmt.Sprintf("volume (0–%d): %s▌", maximum, m.inputText)+mutedStyle.Render("  enter set · esc cancel"), width)
	}
	message := m.message
	if m.errorMsg {
		message = errorStyle.Render(message)
	}
	right := ""
	if m.libraryLoading {
		right = mutedStyle.Render(fmt.Sprintf("%s %s", m.loadPhase, percentText(m.libraryLoaded, m.libraryTotal)))
	}
	return spread(truncate(message, width-lipgloss.Width(right)-1), right, width)
}

func (m model) nowPlayingContent(width int) ([]string, region, region) {
	infoWidth := width - coverColumns - coverGap
	if infoWidth < minimumInfoWidth {
		return m.trackInfo(width)
	}
	info, progress, volume := m.trackInfo(infoWidth)
	cover := strings.Split(m.coverBlock(), "\n")
	lines := make([]string, max(len(cover), len(info)))
	for index := range lines {
		coverLine := strings.Repeat(" ", coverColumns)
		if index < len(cover) {
			coverLine = cover[index]
		}
		infoLine := ""
		if index < len(info) {
			infoLine = info[index]
		}
		lines[index] = coverLine + strings.Repeat(" ", coverGap) + infoLine
	}
	shift := coverColumns + coverGap
	return lines, progress.moved(0, shift), volume.moved(0, shift)
}

func (m model) coverBlock() string {
	switch {
	case m.coverArt != "":
		return m.coverArt
	case m.coverNote != "":
		return coverPlaceholder(m.coverNote)
	case m.trackPath != "":
		return coverPlaceholder("♪")
	}
	return coverPlaceholder("")
}

func (m model) trackInfo(width int) ([]string, region, region) {
	if m.status == nil {
		if m.statusErr != nil {
			return strings.Split(errorStyle.Width(width).Render(m.statusErr.Error()), "\n"), region{}, region{}
		}
		return []string{mutedStyle.Render("Reading player status…")}, region{}, region{}
	}
	status := m.status
	lines := make([]string, infoLineCount)
	lines[0] = stateLabel(status.PlayState)
	var progress region
	if track := status.NowPlaying; track == nil {
		lines[2] = mutedStyle.Render("Nothing is playing")
	} else {
		lines[2] = titleStyle.Render(truncate(track.Title, width))
		lines[3] = truncate(track.Artist, width)
		lines[4] = mutedStyle.Render(truncate(joinNonEmpty(" · ", track.Album, track.Format), width))
		lines[progressLineSlot], progress = progressLine(status.ProgressMs, track.DurationMs, width)
		progress.line = progressLineSlot
	}
	lines[volumeLineSlot] = volumeLine(status.Volume, status.MaxVolume)
	lines[volumeLineSlot+1] = mutedStyle.Render("mode   ") + status.PlayMode
	volume := region{line: volumeLineSlot, start: len(volumeLabel), end: len(volumeLabel) + volumeBarWidth, valid: true}
	return lines, progress, volume
}

func stateLabel(state string) string {
	switch state {
	case "playing":
		return accentStyle.Render("▶ Playing")
	case "paused":
		return mutedStyle.Render("⏸ Paused")
	}
	return mutedStyle.Render("■ " + state)
}

func progressLine(progressMs, durationMs, width int) (string, region) {
	elapsed, total := formatClock(progressMs), formatClock(durationMs)
	barWidth := max(10, width-lipgloss.Width(elapsed)-lipgloss.Width(total)-2)
	start := lipgloss.Width(elapsed) + 1
	line := elapsed + " " + bar(progressMs, durationMs, barWidth, "━", "─") + " " + total
	return line, region{start: start, end: start + barWidth, valid: true}
}

func volumeLine(volume, maximum int) string {
	return mutedStyle.Render(volumeLabel) + bar(volume, maximum, volumeBarWidth, "▮", "▯") + fmt.Sprintf(" %d/%d", volume, maximum)
}

func bar(value, total, width int, filled, empty string) string {
	done := 0
	if total > 0 {
		done = min(width, max(0, value*width/total))
	}
	return accentStyle.Render(strings.Repeat(filled, done)) + mutedStyle.Render(strings.Repeat(empty, width-done))
}

func percentText(loaded, total int) string {
	if total <= 0 {
		return fmt.Sprintf("%d", loaded)
	}
	return fmt.Sprintf("%d/%d · %d%%", loaded, total, loaded*100/total)
}

func (m model) listHeader(width int) []string {
	var lines []string
	if m.libraryLoading {
		label := spread("Reading "+m.loadPhase+" from the player", mutedStyle.Render(percentText(m.libraryLoaded, m.libraryTotal)), width)
		lines = append(lines, label, bar(m.libraryLoaded, max(m.libraryTotal, 1), width, "━", "─"), "")
	}
	if m.screen == albumTracksScreen && m.openAlbum >= 0 && m.openAlbum < len(m.albums) {
		album := m.albums[m.openAlbum]
		lines = append(lines, titleStyle.Render(truncate(album.Name, width-14))+mutedStyle.Render(fmt.Sprintf(" · %d tracks", album.TrackCount)), "")
	}
	return lines
}

func (m model) listContent(width int) ([]string, int, int, int) {
	lines := m.listHeader(width)
	top := len(lines)
	if empty := m.emptyListText(); empty != "" {
		return append(lines, mutedStyle.Render(empty)), top, 0, 0
	}
	visible := m.visible()
	rows := m.rows()
	shown := m.visibleListRows()
	cursor := m.lists[m.screen].cursor
	first := min(max(0, cursor-shown/2), max(0, len(visible)-shown))
	count := min(shown, len(visible)-first)
	for position := first; position < first+count; position++ {
		lines = append(lines, listLine(rows[visible[position]], position == cursor, width))
	}
	return append(lines, mutedStyle.Render(truncate(m.listFooter(len(visible), len(rows)), width))), top, first, count
}

func (m model) emptyListText() string {
	switch {
	case m.screen == albumTracksScreen && m.openAlbumTracks() == nil && m.albumLoading:
		return "Loading the album from the player…"
	case len(m.rows()) == 0 && m.libraryLoading:
		return ""
	case len(m.rows()) == 0:
		return "Nothing here yet · R reads the library from the player"
	case len(m.visible()) == 0:
		return fmt.Sprintf("No matches for %q · esc clears the search", m.lists[m.screen].query)
	}
	return ""
}

func listLine(row listRow, selected bool, width int) string {
	text := truncate(fmt.Sprintf("%5d  %s", row.number, row.text), width-2)
	if selected {
		return cursorStyle.Render("› " + text)
	}
	return "  " + text
}

func (m model) listFooter(shown, total int) string {
	parts := []string{fmt.Sprintf("%d/%d", min(m.lists[m.screen].cursor+1, shown), shown)}
	if query := m.lists[m.screen].query; query != "" && m.input != searchInput {
		parts = append(parts, fmt.Sprintf("filter %q of %d", query, total))
	}
	if !m.librarySavedAt.IsZero() {
		parts = append(parts, "saved "+m.librarySavedAt.Local().Format("2 Jan 15:04"))
	}
	return strings.Join(parts, " · ")
}

func (m model) helpLine() string {
	switch m.screen {
	case nowPlayingScreen:
		return "space play/pause · n/p track · r restart · ←/→ seek · +/- vol · v set volume · m mode · / search · tab next · click bars · wheel volume · q quit"
	case albumTracksScreen:
		return "↑/↓ move · enter play in album · esc back · / search · pgup/pgdn · g/G · tab next · click select, click again play · q quit"
	}
	verb := "play"
	if m.screen == albumsScreen {
		verb = "open"
	}
	return "↑/↓ move · enter " + verb + " · / search · esc clear · pgup/pgdn · g/G · R reload · tab next · click select, click again " + verb + " · wheel scroll · q quit"
}

func formatClock(milliseconds int) string {
	seconds := max(0, milliseconds/1000)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func truncate(text string, width int) string {
	if width <= 1 || lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func joinNonEmpty(separator string, parts ...string) string {
	var kept []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, separator)
}
