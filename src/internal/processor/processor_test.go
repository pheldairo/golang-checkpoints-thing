package processor

import (
	"errors"
	"reflect"
	"setlist/internal/domain"
	"strings"
	"testing"
)

func engine(t *testing.T, tracks ...domain.Track) (*Engine, *domain.Library) {
	t.Helper()
	l := domain.NewLibrary()
	for _, track := range tracks {
		if err := l.Add(track); err != nil {
			t.Fatal(err)
		}
	}
	return New(l), l
}
func TestPlaylistRules(t *testing.T) {
	p, l := engine(t,
		domain.Track{ID: "a", Title: "A", Genre: "Jazz", DurationSeconds: 80, MoodTags: []string{"Calm"}},
		domain.Track{ID: "b", Title: "B", Genre: "jazz", DurationSeconds: 40, MoodTags: []string{"Calm"}},
		domain.Track{ID: "c", Title: "C", Genre: "Rock", DurationSeconds: 10, MoodTags: []string{"Calm"}},
		domain.Track{ID: "d", Title: "D", Genre: "Jazz", DurationSeconds: 20},
	)
	before := l.List()
	in := domain.PlaylistInput{MaxDurationSeconds: 65, MaxTracks: 2, RequiredGenres: []string{" JAZZ "}, PreferredGenres: []string{"Jazz", "jazz"}, PreferredMoodTags: []string{"Calm", "CALM"}}
	r, err := p.Process(domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &in})
	if err != nil {
		t.Fatal(err)
	}
	result := r.Playlist
	if result.TotalDurationSeconds != 60 || result.RemainingDurationSeconds != 5 || result.SelectedCount != 2 || result.Tracks[0].Track.ID != "b" || result.Tracks[1].Track.ID != "d" {
		t.Fatalf("%+v", result)
	}
	if result.Tracks[0].Score != 5 || result.Tracks[1].Score != 3 {
		t.Fatalf("%+v", result.Tracks)
	}
	if !reflect.DeepEqual(result.Tracks[0].Reasons, []string{"preferred genre: jazz (+3)", "preferred mood: Calm (+2)", "fits remaining duration (40 <= 65 seconds)"}) {
		t.Fatal(result.Tracks[0].Reasons)
	}
	if !reflect.DeepEqual(before, l.List()) {
		t.Fatal("processing mutated library")
	}
	for n := 0; n < 5; n++ {
		again, _ := p.Process(domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &in})
		if !reflect.DeepEqual(again, r) {
			t.Fatal("nondeterministic playlist")
		}
	}
	in.MaxTracks = 1
	limited, _ := p.Process(domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &in})
	if limited.Playlist.SelectedCount != 1 {
		t.Fatal("track cap")
	}
	in.RequiredGenres = []string{"Ambient"}
	empty, _ := p.Process(domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &in})
	if empty.Playlist.SelectedCount != 0 || empty.Playlist.TotalDurationSeconds != 0 || empty.Playlist.RemainingDurationSeconds != 65 {
		t.Fatalf("%+v", empty)
	}
}
func TestPlaylistMoodAndTieOrder(t *testing.T) {
	p, _ := engine(t,
		domain.Track{ID: "c", Title: "C", DurationSeconds: 10},
		domain.Track{ID: "b", Title: "B", DurationSeconds: 10, MoodTags: []string{"Relaxed"}},
		domain.Track{ID: "a", Title: "A", DurationSeconds: 10, MoodTags: []string{"Relaxed"}},
	)
	in := domain.PlaylistInput{MaxDurationSeconds: 100, MaxTracks: 3, PreferredMoodTags: []string{"relaxed"}}
	result := p.playlist(in)
	if result.Tracks[0].Track.ID != "a" || result.Tracks[1].Track.ID != "b" || result.Tracks[2].Track.ID != "c" || result.Tracks[0].Score != 2 || result.Tracks[2].Score != 0 || !strings.Contains(result.Tracks[2].Reasons[0], "no preference matches") {
		t.Fatalf("%+v", result.Tracks)
	}
	for duration := 1; duration <= 35; duration++ {
		in.MaxDurationSeconds = duration
		result = p.playlist(in)
		if result.TotalDurationSeconds > duration || result.RemainingDurationSeconds < 0 {
			t.Fatal("duration exceeded")
		}
	}
}
func TestAnalysis(t *testing.T) {
	p, l := engine(t,
		domain.Track{ID: "a", Title: "Song", Artist: "Artist", Album: "Album", Genre: "Jazz", DurationSeconds: 10},
		domain.Track{ID: "b", Title: " SONG ", Artist: "ARTIST", Album: " album ", Genre: "JAZZ", DurationSeconds: 20},
		domain.Track{ID: "c", Title: "Other", Artist: "Second", Album: "Other", Genre: "Rock", DurationSeconds: 30},
		domain.Track{ID: "d", Title: "Fallback", Artist: "Unknown", Album: "Folder", DurationSeconds: 40, MissingMetadata: []string{"title", "album", "artist", "genre"}},
	)
	before := l.List()
	r := p.analysis(domain.AnalysisInput{DetectDuplicates: true, IncludeCompleteness: true})
	if r.TotalTracks != 4 || r.UniqueArtists != 2 || r.UniqueAlbums != 3 || r.TotalDurationSeconds != 100 {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(r.Genres, []domain.GenreCount{{Genre: "Jazz", Count: 2}, {Genre: "Rock", Count: 1}, {Genre: "Unknown", Count: 1}}) {
		t.Fatal(r.Genres)
	}
	if !reflect.DeepEqual(r.DuplicateGroups, [][]string{{"a", "b"}}) {
		t.Fatal(r.DuplicateGroups)
	}
	if !reflect.DeepEqual(r.MissingMetadata, []domain.IncompleteTrack{{TrackID: "d", Fields: []string{"album", "artist", "genre", "title"}}}) {
		t.Fatal(r.MissingMetadata)
	}
	if !reflect.DeepEqual(before, l.List()) {
		t.Fatal("analysis mutated library")
	}
	off := p.analysis(domain.AnalysisInput{})
	if len(off.MissingMetadata) != 0 || len(off.DuplicateGroups) != 0 || off.TotalTracks != 4 {
		t.Fatal(off)
	}
	empty, _ := engine(t)
	report := empty.analysis(domain.AnalysisInput{DetectDuplicates: true})
	if report.TotalTracks != 0 || report.UniqueAlbums != 0 || len(report.Genres) != 0 {
		t.Fatal(report)
	}
}
func TestDiscovery(t *testing.T) {
	p, l := engine(t,
		domain.Track{ID: "ref", Title: "Reference", Artist: "Band", Album: "Record", Genre: "Jazz", DurationSeconds: 10, MoodTags: []string{"Calm", "Warm"}},
		domain.Track{ID: "b", Title: "B", Artist: "band", Album: "record", Genre: "jazz", DurationSeconds: 10, MoodTags: []string{"calm", "warm"}},
		domain.Track{ID: "a", Title: "A", Artist: "Other", Genre: "Jazz", DurationSeconds: 10, MoodTags: []string{"Calm"}},
		domain.Track{ID: "c", Title: "C", Genre: "Jazz", DurationSeconds: 10, MoodTags: []string{"Calm"}},
		domain.Track{ID: "none", Title: "None", Artist: "Unknown", Genre: "Unknown", DurationSeconds: 10},
	)
	before := l.List()
	in := domain.TaskInput{Kind: domain.DiscoverSimilarTracks, Similarity: &domain.SimilarityInput{ReferenceTrackID: "ref", MaxMatches: 10}}
	r, err := p.Process(in)
	if err != nil {
		t.Fatal(err)
	}
	matches := r.Similarity.Matches
	if len(matches) != 3 || matches[0].Track.ID != "b" || matches[0].Score != 10 || matches[1].Track.ID != "a" || matches[1].Score != 5 || matches[2].Track.ID != "c" || len(matches[0].Reasons) != 5 {
		t.Fatalf("%+v", matches)
	}
	if !reflect.DeepEqual(matches[0].Reasons, []string{"same genre (+3)", "shared mood: Calm (+2)", "shared mood: Warm (+2)", "same artist (+2)", "same album (+1)"}) {
		t.Fatal(matches[0].Reasons)
	}
	for _, m := range matches {
		if m.Track.ID == "ref" {
			t.Fatal("reference included")
		}
	}
	in.Similarity.MaxMatches = 1
	limited, err := p.Process(in)
	if err != nil || len(limited.Similarity.Matches) != 1 {
		t.Fatal(limited, err)
	}
	in.Similarity.ReferenceTrackID = "missing"
	if _, err = p.Process(in); !errors.Is(err, domain.ErrTrackNotFound) {
		t.Fatal(err)
	}
	in.Similarity.MaxMatches = 0
	if _, err = p.Process(in); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, l.List()) {
		t.Fatal("discovery mutated library")
	}
}
func TestMissingMetadataDoesNotMatch(t *testing.T) {
	p, _ := engine(t, domain.Track{ID: "a", Title: "A", DurationSeconds: 10}, domain.Track{ID: "b", Title: "B", Artist: "Unknown", Album: "Unknown", Genre: "Unknown", DurationSeconds: 10, MoodTags: []string{""}})
	r, err := p.discovery(domain.SimilarityInput{ReferenceTrackID: "a", MaxMatches: 2})
	if err != nil || len(r.Matches) != 0 {
		t.Fatal(r, err)
	}
}
