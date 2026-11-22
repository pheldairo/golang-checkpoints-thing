package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	coverColumns  = 24
	coverRows     = 12
	coverSettle   = 2500 * time.Millisecond
	coverRetries  = 3
	upperHalfCell = "▀"
)

type coverMsg struct {
	path string
	art  string
	note string
	err  error
}

func fetchCover(remote Remote, path string) tea.Cmd {
	return func() tea.Msg {
		data, err := remote.Cover()
		if err != nil {
			return coverMsg{path: path, err: err}
		}
		if data == nil {
			return coverMsg{path: path}
		}
		art, err := renderCover(data, coverColumns, coverRows)
		if err != nil {
			return coverMsg{path: path, note: "unreadable cover"}
		}
		return coverMsg{path: path, art: art}
	}
}

func renderCover(data []byte, columns, rows int) (string, error) {
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode cover: %w", err)
	}
	pixels := coverPixels(decoded, columns, rows*2)
	lines := make([]string, rows)
	for row := range rows {
		var line strings.Builder
		for column := range columns {
			cell := lipgloss.NewStyle().
				Foreground(hexColor(pixels[row*2][column])).
				Background(hexColor(pixels[row*2+1][column]))
			line.WriteString(cell.Render(upperHalfCell))
		}
		lines[row] = line.String()
	}
	return strings.Join(lines, "\n"), nil
}

func coverPixels(source image.Image, width, height int) [][]color.RGBA {
	square := centeredSquare(source.Bounds())
	pixels := make([][]color.RGBA, height)
	for row := range height {
		pixels[row] = make([]color.RGBA, width)
		flippedRow := height - 1 - row
		for column := range width {
			area := image.Rect(
				square.Min.X+column*square.Dx()/width,
				square.Min.Y+flippedRow*square.Dy()/height,
				square.Min.X+(column+1)*square.Dx()/width,
				square.Min.Y+(flippedRow+1)*square.Dy()/height,
			)
			pixels[row][column] = averageColor(source, area)
		}
	}
	return pixels
}

func centeredSquare(bounds image.Rectangle) image.Rectangle {
	side := min(bounds.Dx(), bounds.Dy())
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	return image.Rect(left, top, left+side, top+side)
}

func averageColor(source image.Image, area image.Rectangle) color.RGBA {
	if area.Empty() {
		area = image.Rect(area.Min.X, area.Min.Y, area.Min.X+1, area.Min.Y+1)
	}
	var red, green, blue, count uint64
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			r, g, b, _ := source.At(x, y).RGBA()
			red, green, blue = red+uint64(r>>8), green+uint64(g>>8), blue+uint64(b>>8)
			count++
		}
	}
	return color.RGBA{R: uint8(red / count), G: uint8(green / count), B: uint8(blue / count), A: 0xff}
}

func hexColor(pixel color.RGBA) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", pixel.R, pixel.G, pixel.B))
}

func coverPlaceholder(note string) string {
	return lipgloss.NewStyle().
		Width(coverColumns).
		Height(coverRows).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(muted).
		Background(placeholderShade).
		Render(note)
}
