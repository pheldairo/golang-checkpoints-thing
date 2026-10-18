package service

import (
	"errors"
	"setlist/internal/domain"
	"setlist/internal/processor"
	"setlist/internal/storage"
	"testing"
)

type processorFunc func(domain.TaskInput) (domain.TaskResult, error)

func (f processorFunc) Process(in domain.TaskInput) (domain.TaskResult, error) { return f(in) }
func setup(t *testing.T) (*TaskService, *storage.Memory) {
	t.Helper()
	l := domain.NewLibrary()
	if err := l.Add(domain.Track{ID: "a", Title: "A", DurationSeconds: 20, MoodTags: []string{"Calm"}}); err != nil {
		t.Fatal(err)
	}
	store := storage.NewMemory()
	return New(store, processor.New(l)), store
}
func playlist(seconds int) domain.TaskInput {
	return domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &domain.PlaylistInput{MaxTracks: 2, MaxDurationSeconds: seconds}}
}
func TestLifecycleAndIndependentResults(t *testing.T) {
	s, store := setup(t)
	if _, err := s.Create(playlist(0)); !errors.Is(err, domain.ErrInvalidInput) || len(store.List()) != 0 {
		t.Fatalf("invalid create: %v", err)
	}
	if _, err := s.Get("missing"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Process("missing"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Fatal(err)
	}
	input := playlist(25)
	input.Playlist.PreferredMoodTags = []string{"Calm"}
	first, err := s.Create(input)
	if err != nil || first.Status != domain.StatusPending || first.Result != nil {
		t.Fatalf("%+v %v", first, err)
	}
	input.Playlist.PreferredMoodTags[0] = "changed"
	first.Input.Playlist.MaxDurationSeconds = 1
	got, err := s.Get(first.ID)
	if err != nil || got.Input.Playlist.MaxDurationSeconds != 25 || got.Input.Playlist.PreferredMoodTags[0] != "Calm" {
		t.Fatalf("ownership: %+v %v", got, err)
	}
	second, err := s.Create(playlist(10))
	if err != nil || second.ID == first.ID {
		t.Fatalf("%+v %v", second, err)
	}
	result, err := s.Process(first.ID)
	if err != nil || result.Status != domain.StatusCompleted || result.Result.Playlist.TotalDurationSeconds != 20 || result.Result.Playlist.RemainingDurationSeconds != 5 || result.Result.Playlist.SelectedCount != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	result.Result.Playlist.Tracks[0].Track.MoodTags[0] = "changed"
	result.Result.Playlist.TotalDurationSeconds = 999
	saved, _ := s.Get(first.ID)
	if saved.Result.Playlist.TotalDurationSeconds != 20 || saved.Result.Playlist.Tracks[0].Track.MoodTags[0] != "Calm" {
		t.Fatal("result leaked")
	}
	other, err := s.Process(second.ID)
	if err != nil || other.Result.Playlist.SelectedCount != 0 || other.Result.Playlist.RemainingDurationSeconds != 10 {
		t.Fatalf("%+v %v", other, err)
	}
	saved, _ = s.Get(first.ID)
	if saved.Result.Playlist.SelectedCount != 1 {
		t.Fatal("independent results overwritten")
	}
	if _, err = s.Process(first.ID); !errors.Is(err, domain.ErrAlreadyProcessed) {
		t.Fatal(err)
	}
	if len(store.List()) != 2 {
		t.Fatal("task count")
	}
}
func TestFailedProcessingAndProcessingStatus(t *testing.T) {
	store := storage.NewMemory()
	failure := errors.New("processor failed")
	s := New(store, processorFunc(func(in domain.TaskInput) (domain.TaskResult, error) {
		tasks := store.List()
		if len(tasks) != 1 || tasks[0].Status != domain.StatusProcessing {
			t.Fatal("processor did not observe processing state")
		}
		return domain.TaskResult{}, failure
	}))
	task, err := s.Create(playlist(10))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.Process(task.ID)
	if !errors.Is(err, failure) || failed.Status != domain.StatusFailed || failed.Error != "processor failed" || failed.Result != nil {
		t.Fatalf("%+v %v", failed, err)
	}
	saved, err := s.Get(task.ID)
	if err != nil || saved.Error != failed.Error || saved.Status != domain.StatusFailed {
		t.Fatalf("%+v %v", saved, err)
	}
	if _, err = s.Process(task.ID); !errors.Is(err, domain.ErrAlreadyProcessed) {
		t.Fatal(err)
	}
}
func TestMissingReferenceBecomesFailedTask(t *testing.T) {
	s, _ := setup(t)
	task, err := s.Create(domain.TaskInput{Kind: domain.DiscoverSimilarTracks, Similarity: &domain.SimilarityInput{ReferenceTrackID: "absent", MaxMatches: 2}})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.Process(task.ID)
	if !errors.Is(err, domain.ErrTrackNotFound) || failed.Status != domain.StatusFailed || failed.Error == "" {
		t.Fatalf("%+v %v", failed, err)
	}
}
func TestInvalidProcessorResultFails(t *testing.T) {
	s := New(storage.NewMemory(), processorFunc(func(domain.TaskInput) (domain.TaskResult, error) {
		return domain.TaskResult{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisResult{}}, nil
	}))
	task, err := s.Create(playlist(10))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.Process(task.ID)
	if !errors.Is(err, domain.ErrInvalidResult) || failed.Status != domain.StatusFailed {
		t.Fatalf("%+v %v", failed, err)
	}
}
