package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrInvalidTrack      = errors.New("invalid track")
	ErrTrackNotFound     = errors.New("track not found")
	ErrDuplicateTrack    = errors.New("duplicate track ID")
	ErrInvalidResult     = errors.New("invalid processor result")
	ErrInvalidInput      = errors.New("invalid task input")
	ErrTaskNotFound      = errors.New("task not found")
	ErrDuplicateTask     = errors.New("duplicate task ID")
	ErrInvalidTransition = errors.New("invalid task transition")
	ErrAlreadyProcessed  = errors.New("task already processed")
)

// Normalize is used for comparisons only; display values retain their spelling.
func Normalize(s string) string {
	return strings.ToLower(Clean(s))
}

func Clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func Known(s string) bool {
	n := Normalize(s)
	return n != "" && n != "unknown"
}

type Track struct {
	ID              string
	Title           string
	Artist          string
	Album           string
	Genre           string
	DurationSeconds int
	TrackNumber     int
	MoodTags        []string
	SourcePath      string
	// MissingMetadata records absent embedded fields even when fallbacks are displayed.
	MissingMetadata []string
}

func (t Track) Validate() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Title) == "" || t.DurationSeconds <= 0 || t.TrackNumber < 0 {
		return fmt.Errorf("%w: ID, title and positive duration required; track number cannot be negative", ErrInvalidTrack)
	}
	return nil
}

func (t Track) Clone() Track {
	t.MoodTags = append([]string(nil), t.MoodTags...)
	t.MissingMetadata = append([]string(nil), t.MissingMetadata...)
	return t
}

func CleanTags(tags []string) []string {
	seen := make(map[string]bool)
	out := []string{}
	for _, tag := range tags {
		tag = Clean(tag)
		key := Normalize(tag)
		if Known(tag) && !seen[key] {
			seen[key] = true
			out = append(out, tag)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return Normalize(out[i]) < Normalize(out[j])
	})
	return out
}
