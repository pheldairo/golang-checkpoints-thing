package storage

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"setlist/internal/domain"
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

func pendingTask(id string) domain.Task {
	return domain.Task{ID: id, Status: domain.StatusPending, Input: domain.TaskInput{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisInput{}}}
}

func TestEnqueueIsAtomicUnderConcurrency(t *testing.T) {
	m := NewMemory()
	if err := m.Add(pendingTask("a")); err != nil {
		t.Fatal(err)
	}
	const attempts = 50
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Enqueue("a"); err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, domain.ErrInvalidState) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("task was enqueued %d times", accepted.Load())
	}
}

func TestQueuedTaskOnlyStartsThroughTheQueue(t *testing.T) {
	m := NewMemory()
	if err := m.Add(pendingTask("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartQueued("a"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("pending task started as queued: %v", err)
	}
	if _, err := m.Enqueue("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("a"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("queued task started synchronously: %v", err)
	}
	task, err := m.StartQueued("a")
	if err != nil || task.Status != domain.StatusProcessing {
		t.Fatalf("%+v %v", task, err)
	}
	if _, err := m.StartQueued("a"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("task started twice: %v", err)
	}
}

func TestConcurrentReadsAndWritesAreSafe(t *testing.T) {
	m := NewMemory()
	var wg sync.WaitGroup
	for index := range 20 {
		id := fmt.Sprintf("t%d", index)
		if err := m.Add(pendingTask(id)); err != nil {
			t.Fatal(err)
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := m.Start(id); err != nil {
				t.Error(err)
				return
			}
			result := domain.TaskResult{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisResult{TotalTracks: index}}
			if _, err := m.Finish(id, result, nil); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			_ = m.List()
			_, _ = m.Get(id)
		}()
	}
	wg.Wait()
	for index, task := range m.List() {
		if task.Status != domain.StatusCompleted || task.Result.Analysis.TotalTracks != index {
			t.Fatalf("task %s has %+v", task.ID, task.Result)
		}
	}
}
