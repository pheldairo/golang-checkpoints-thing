package storage

import (
	"errors"
	"setlist/internal/domain"
	"testing"
)

func TestMemoryRejectsDuplicatesAndInvalidTransitions(t *testing.T) {
	m := NewMemory()
	task := domain.Task{ID: "a", Status: domain.StatusPending, Input: domain.TaskInput{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisInput{}}}
	if err := m.Add(task); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(task); !errors.Is(err, domain.ErrDuplicateTask) {
		t.Fatal(err)
	}
	if _, err := m.Finish("a", domain.TaskResult{}, errors.New("failure")); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal(err)
	}
	if _, err := m.Start("missing"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Fatal(err)
	}
	if _, err := m.Start("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("a"); !errors.Is(err, domain.ErrAlreadyProcessed) {
		t.Fatal(err)
	}
	task, err := m.Finish("a", domain.TaskResult{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisResult{TotalTracks: 2}}, nil)
	if err != nil || task.Status != domain.StatusCompleted {
		t.Fatalf("%+v %v", task, err)
	}
	if _, err = m.Finish("a", domain.TaskResult{}, errors.New("fail")); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal(err)
	}
	task.ID = "b"
	if err = m.Add(task); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal(err)
	}
}
