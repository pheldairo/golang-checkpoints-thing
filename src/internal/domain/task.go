package domain

import (
	"fmt"
	"strings"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusProcessing TaskStatus = "processing"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
)

type TaskType string

const (
	GeneratePlaylist      TaskType = "generate_playlist"
	AnalyzeLibrary        TaskType = "analyze_library"
	DiscoverSimilarTracks TaskType = "discover_similar_tracks"
)

type PlaylistInput struct {
	MaxDurationSeconds int
	RequiredGenres     []string
	PreferredGenres    []string
	PreferredMoodTags  []string
	MaxTracks          int
}

type AnalysisInput struct {
	DetectDuplicates    bool
	IncludeCompleteness bool
}

type SimilarityInput struct {
	ReferenceTrackID string
	MaxMatches       int
}

type TaskInput struct {
	Kind       TaskType
	Playlist   *PlaylistInput   `json:",omitempty"`
	Analysis   *AnalysisInput   `json:",omitempty"`
	Similarity *SimilarityInput `json:",omitempty"`
}

func (in TaskInput) Validate() error {
	count := 0
	if in.Playlist != nil {
		count++
	}
	if in.Analysis != nil {
		count++
	}
	if in.Similarity != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: exactly one operation input required", ErrInvalidInput)
	}
	switch in.Kind {
	case GeneratePlaylist:
		if in.Playlist == nil || in.Playlist.MaxDurationSeconds <= 0 || in.Playlist.MaxTracks <= 0 {
			return fmt.Errorf("%w: positive playlist duration and track limit required", ErrInvalidInput)
		}
		for _, values := range [][]string{in.Playlist.RequiredGenres, in.Playlist.PreferredGenres, in.Playlist.PreferredMoodTags} {
			for _, v := range values {
				if !Known(v) {
					return fmt.Errorf("%w: genre/mood values cannot be blank or unknown", ErrInvalidInput)
				}
			}
		}
	case AnalyzeLibrary:
		if in.Analysis == nil {
			return fmt.Errorf("%w: analysis input required", ErrInvalidInput)
		}
	case DiscoverSimilarTracks:
		if in.Similarity == nil || strings.TrimSpace(in.Similarity.ReferenceTrackID) == "" || in.Similarity.MaxMatches <= 0 {
			return fmt.Errorf("%w: reference ID and positive match limit required", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: unsupported operation %q", ErrInvalidInput, in.Kind)
	}
	return nil
}

type Task struct {
	ID     string
	Input  TaskInput
	Status TaskStatus
	Result *TaskResult `json:",omitempty"`
	Error  string      `json:",omitempty"`
}

func (t *Task) Transition(next TaskStatus) error {
	valid := t.Status == StatusPending && next == StatusProcessing ||
		t.Status == StatusProcessing && (next == StatusCompleted || next == StatusFailed)
	if !valid {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, next)
	}
	t.Status = next
	return nil
}

func (t Task) Clone() Task {
	if t.Input.Playlist != nil {
		input := *t.Input.Playlist
		input.RequiredGenres = append([]string(nil), input.RequiredGenres...)
		input.PreferredGenres = append([]string(nil), input.PreferredGenres...)
		input.PreferredMoodTags = append([]string(nil), input.PreferredMoodTags...)
		t.Input.Playlist = &input
	}
	if t.Input.Analysis != nil {
		input := *t.Input.Analysis
		t.Input.Analysis = &input
	}
	if t.Input.Similarity != nil {
		input := *t.Input.Similarity
		t.Input.Similarity = &input
	}
	if t.Result != nil {
		result := t.Result.Clone()
		t.Result = &result
	}
	return t
}
