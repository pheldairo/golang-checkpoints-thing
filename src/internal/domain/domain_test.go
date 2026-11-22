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
	statuses := []TaskStatus{StatusPending, StatusQueued, StatusProcessing, StatusCompleted, StatusFailed, "bogus"}
	allowed := map[[2]TaskStatus]bool{
		{StatusPending, StatusQueued}:       true,
		{StatusPending, StatusProcessing}:   true,
		{StatusQueued, StatusProcessing}:    true,
		{StatusProcessing, StatusCompleted}: true,
		{StatusProcessing, StatusFailed}:    true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			t.Run(string(from)+"/"+string(to), func(t *testing.T) {
				task := Task{Status: from}
				err := task.Transition(to)
				valid := allowed[[2]TaskStatus{from, to}]
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

func TestPlayerInputValidation(t *testing.T) {
	valid := []PlayerInput{
		{Action: ActionStatus},
		{Action: ActionSetVolume, Volume: 20},
		{Action: ActionSetVolume, Volume: -2, RelativeVolume: true},
		{Action: ActionSeek, PositionMs: 30000},
		{Action: ActionSetPlayMode, PlayMode: PlayModeShuffle},
		{Action: ActionLibrary, Offset: 5, Limit: 10},
		{Action: ActionPlayTrack, TrackIndex: 3},
	}
	for _, input := range valid {
		task := TaskInput{Kind: PlayerControl, Player: &input}
		if err := task.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", input, err)
		}
	}
	invalid := []PlayerInput{
		{Action: "dance"},
		{Action: ActionSetVolume, Volume: 300},
		{Action: ActionSetVolume, RelativeVolume: true},
		{Action: ActionSeek, PositionMs: -1},
		{Action: ActionSetPlayMode, PlayMode: 9},
		{Action: ActionLibrary, Limit: 0},
		{Action: ActionLibrary, Limit: 51},
		{Action: ActionPlayTrack, TrackIndex: -1},
	}
	for _, input := range invalid {
		task := TaskInput{Kind: PlayerControl, Player: &input}
		if !errors.Is(task.Validate(), ErrInvalidInput) {
			t.Errorf("%+v accepted", input)
		}
	}
	if !errors.Is((TaskInput{Kind: PlayerControl}).Validate(), ErrInvalidInput) {
		t.Error("player task without input accepted")
	}
}

func TestPlayerResultCloneIsDeep(t *testing.T) {
	original := TaskResult{Kind: PlayerControl, Player: &PlayerResult{
		Action:   ActionSeek,
		Playback: &PlaybackChange{ProgressSamplesMs: []int{1, 2}},
		Track:    &TrackChange{Before: &PlayerTrack{Title: "A"}},
		Library:  []PlayerLibraryItem{{Title: "B"}},
	}}
	copied := original.Clone()
	copied.Player.Playback.ProgressSamplesMs[0] = 99
	copied.Player.Track.Before.Title = "changed"
	copied.Player.Library[0].Title = "changed"
	if original.Player.Playback.ProgressSamplesMs[0] != 1 || original.Player.Track.Before.Title != "A" || original.Player.Library[0].Title != "B" {
		t.Fatal("player result clone shares mutable data")
	}
}

func TestPlayTrackWithLocatorAcceptsAnyLibraryPosition(t *testing.T) {
	locator := `<path>a:\x.flac<cue_id>-1<name>x`
	valid := []PlayerInput{
		{Action: ActionPlayTrack, TrackIndex: 2971, Locator: locator},
		{Action: ActionPlayTrack, TrackIndex: 4, Locator: locator, Album: "Abbey Road"},
	}
	for _, input := range valid {
		if err := input.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", input, err)
		}
	}
	invalid := []PlayerInput{
		{Action: ActionPlayTrack, TrackIndex: 2971},
		{Action: ActionPlayTrack, TrackIndex: -1, Locator: locator},
		{Action: ActionPlayTrack, Album: "Abbey Road"},
	}
	for _, input := range invalid {
		if !errors.Is(input.Validate(), ErrInvalidInput) {
			t.Errorf("%+v accepted", input)
		}
	}
}
