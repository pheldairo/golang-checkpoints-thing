package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestLibrary(t *testing.T) {
	l := NewLibrary()
	track := Track{ID: "b", Title: " Song ", Genre: " Jazz ", DurationSeconds: 10, MoodTags: []string{"Calm"}}
	if err := l.Add(track); err != nil {
		t.Fatal(err)
	}
	track.MoodTags[0] = "changed"
	got, err := l.Get("b")
	if err != nil || got.Title != "Song" || got.MoodTags[0] != "Calm" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got.MoodTags[0] = "mutated"
	list := l.List()
	list[0].MoodTags[0] = "mutated"
	again, _ := l.Get("b")
	if again.MoodTags[0] != "Calm" {
		t.Fatal("library leaked mutable data")
	}
	if err = l.Add(track); !errors.Is(err, ErrDuplicateTrack) {
		t.Fatal(err)
	}
	if _, err = l.Get("missing"); !errors.Is(err, ErrTrackNotFound) {
		t.Fatal(err)
	}
	if err = l.Add(Track{ID: "a", Title: "Other", DurationSeconds: 3}); err != nil {
		t.Fatal(err)
	}
	if l.List()[0].ID != "a" || len(l.FilterGenres([]string{" JAZZ "})) != 1 {
		t.Fatal("ordering/filter")
	}
}
func TestTrackValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		track Track
		valid bool
	}{
		{"valid", Track{ID: "a", Title: "song", DurationSeconds: 1}, true},
		{"missing ID", Track{Title: "song", DurationSeconds: 1}, false},
		{"missing title", Track{ID: "a", DurationSeconds: 1}, false},
		{"zero duration", Track{ID: "a", Title: "song"}, false},
		{"negative duration", Track{ID: "a", Title: "song", DurationSeconds: -1}, false},
		{"negative number", Track{ID: "a", Title: "song", DurationSeconds: 1, TrackNumber: -1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.track.Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("%v", err)
			}
		})
	}
}
func TestTransitions(t *testing.T) {
	statuses := []TaskStatus{StatusPending, StatusProcessing, StatusCompleted, StatusFailed, "bogus"}
	for _, from := range statuses {
		for _, to := range statuses {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				task := Task{Status: from}
				err := task.Transition(to)
				valid := from == StatusPending && to == StatusProcessing || from == StatusProcessing && (to == StatusCompleted || to == StatusFailed)
				if valid {
					if err != nil || task.Status != to {
						t.Fatalf("%+v %v", task, err)
					}
				} else if !errors.Is(err, ErrInvalidTransition) || task.Status != from {
					t.Fatalf("invalid transition changed task: %+v %v", task, err)
				}
			})
		}
	}
}
func TestInputValidation(t *testing.T) {
	for _, in := range []TaskInput{
		{}, {Kind: GeneratePlaylist, Playlist: &PlaylistInput{}},
		{Kind: AnalyzeLibrary, Playlist: &PlaylistInput{MaxTracks: 1, MaxDurationSeconds: 10}},
		{Kind: AnalyzeLibrary, Analysis: &AnalysisInput{}, Similarity: &SimilarityInput{}},
		{Kind: DiscoverSimilarTracks, Similarity: &SimilarityInput{ReferenceTrackID: "a", MaxMatches: 0}},
		{Kind: DiscoverSimilarTracks, Similarity: &SimilarityInput{ReferenceTrackID: " ", MaxMatches: 1}},
		{Kind: GeneratePlaylist, Playlist: &PlaylistInput{MaxTracks: 1, MaxDurationSeconds: 10, PreferredGenres: []string{" "}}},
		{Kind: "unknown", Analysis: &AnalysisInput{}},
	} {
		if !errors.Is(in.Validate(), ErrInvalidInput) {
			t.Fatalf("accepted %+v", in)
		}
	}
	if got := CleanTags([]string{" Calm ", "calm", "", "Unknown", "Bright"}); !reflect.DeepEqual(got, []string{"Bright", "Calm"}) {
		t.Fatal(got)
	}
}
