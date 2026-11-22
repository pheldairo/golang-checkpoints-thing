package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	defaultPanelWidth = 84
	minimumPanelWidth = 44
	maximumPanelWidth = 100
	panelTopLine      = 1
	contentTopLine    = 2
	layoutMargin      = 1
)

type region struct {
	line  int
	start int
	end   int
	valid bool
}

func (r region) contains(line, column int) bool {
	return r.valid && line == r.line && column >= r.start && column < r.end
}

func (r region) moved(lines, columns int) region {
	r.line += lines
	r.start += columns
	r.end += columns
	return r
}

type frame struct {
	lines     []string
	tabs      []region
	progress  region
	volume    region
	listTop   int
	listFirst int
	listCount int
}

func (m model) panelWidth() int {
	if m.width == 0 {
		return defaultPanelWidth
	}
	return max(minimumPanelWidth, min(m.width-4, maximumPanelWidth))
}

func contentLeft() int {
	return panelStyle.GetBorderLeftSize() + panelStyle.GetPaddingLeft()
}

func (m model) frame() frame {
	width := m.panelWidth()
	inner := width - panelStyle.GetHorizontalFrameSize()
	header, tabs := m.header(width)
	f := frame{tabs: tabs}
	var content []string
	if m.screen == nowPlayingScreen {
		var progress, volume region
		content, progress, volume = m.nowPlayingContent(inner)
		f.progress = progress.moved(contentTopLine, contentLeft())
		f.volume = volume.moved(contentTopLine, contentLeft())
	} else {
		var listTop int
		content, listTop, f.listFirst, f.listCount = m.listContent(inner)
		f.listTop = contentTopLine + listTop
	}
	panel := panelStyle.Width(width - panelStyle.GetHorizontalBorderSize()).Render(strings.Join(content, "\n"))
	f.lines = append([]string{header}, strings.Split(panel, "\n")...)
	f.lines = append(f.lines, m.footer(width))
	f.lines = append(f.lines, strings.Split(m.helpBlock(width), "\n")...)
	return f
}

func (f frame) width() int {
	widest := 0
	for _, line := range f.lines {
		widest = max(widest, lipgloss.Width(line))
	}
	return widest
}

func (m model) offsets(f frame) (int, int) {
	left, top := 0, 0
	if m.width > 0 {
		left = max(0, (m.width-f.width())/2)
	}
	if m.height > 0 {
		top = max(0, (m.height-len(f.lines))/2)
	}
	return left, top
}

func (m model) View() string {
	f := m.frame()
	left, top := m.offsets(f)
	indent := strings.Repeat(" ", left)
	lines := make([]string, 0, max(m.height, len(f.lines)))
	for range top {
		lines = append(lines, "")
	}
	for _, line := range f.lines {
		lines = append(lines, indent+line)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m model) helpBlock(width int) string {
	return mutedStyle.Width(width).Render(m.helpLine())
}

func (m model) visibleListRows() int {
	if m.height == 0 {
		return coverRows
	}
	width := m.panelWidth()
	reserved := 1 + panelStyle.GetVerticalBorderSize() + len(m.listHeader(width)) + 1 + 1 + lipgloss.Height(m.helpBlock(width)) + layoutMargin
	return max(3, m.height-reserved)
}
