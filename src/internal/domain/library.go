package domain

import (
	"fmt"
	"sort"
)

type Library struct {
	tracks map[string]Track
}

func NewLibrary() *Library {
	return &Library{tracks: make(map[string]Track)}
}

func (l *Library) Add(t Track) error {
	t.ID = Clean(t.ID)
	t.Title = Clean(t.Title)
	t.Artist = Clean(t.Artist)
	t.Album = Clean(t.Album)
	t.Genre = Clean(t.Genre)
	t.MoodTags = CleanTags(t.MoodTags)
	if err := t.Validate(); err != nil {
		return err
	}
	if _, ok := l.tracks[t.ID]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateTrack, t.ID)
	}
	if l.tracks == nil {
		l.tracks = make(map[string]Track)
	}
	l.tracks[t.ID] = t.Clone()
	return nil
}

func (l *Library) Get(id string) (Track, error) {
	t, ok := l.tracks[id]
	if !ok {
		return Track{}, fmt.Errorf("%w: %s", ErrTrackNotFound, id)
	}
	return t.Clone(), nil
}

func (l *Library) List() []Track {
	out := make([]Track, 0, len(l.tracks))
	for _, t := range l.tracks {
		out = append(out, t.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Contains(values []string, value string) bool {
	if !Known(value) {
		return false
	}
	for _, v := range values {
		if Known(v) && Normalize(v) == Normalize(value) {
			return true
		}
	}
	return false
}

func (l *Library) FilterGenres(genres []string) []Track {
	out := []Track{}
	for _, t := range l.List() {
		if len(genres) == 0 || Contains(genres, t.Genre) {
			out = append(out, t)
		}
	}
	return out
}
