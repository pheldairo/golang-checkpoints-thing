package service

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"setlist/internal/domain"
	"setlist/internal/storage"
)

const (
	failingTag  = 13
	testTimeout = 3 * time.Second
)

type gatedProcessor struct {
	started  chan int
	release  chan struct{}
	inFlight atomic.Int64
	peak     atomic.Int64
	calls    atomic.Int64
}

func newGatedProcessor() *gatedProcessor {
	return &gatedProcessor{started: make(chan int, 1000), release: make(chan struct{})}
}

func openGate() *gatedProcessor {
	processor := newGatedProcessor()
	close(processor.release)
	return processor
}

func (g *gatedProcessor) Process(input domain.TaskInput) (domain.TaskResult, error) {
	g.calls.Add(1)
	current := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		peak := g.peak.Load()
		if current <= peak || g.peak.CompareAndSwap(peak, current) {
			break
		}
	}
	tag := input.Playlist.MaxTracks
	g.started <- tag
	<-g.release
	if tag == failingTag {
		return domain.TaskResult{}, errors.New("tag 13 always fails")
	}
	return domain.TaskResult{Kind: domain.GeneratePlaylist, Playlist: &domain.PlaylistResult{SelectedCount: tag}}, nil
}

func (g *gatedProcessor) openGate() {
	close(g.release)
}

func (g *gatedProcessor) waitForStarts(t *testing.T, count int) []int {
	t.Helper()
	var tags []int
	for range count {
		select {
		case tag := <-g.started:
			tags = append(tags, tag)
		case <-time.After(testTimeout):
			t.Fatalf("only %d of %d tasks started processing", len(tags), count)
		}
	}
	return tags
}

func taggedInput(tag int) domain.TaskInput {
	return domain.TaskInput{Kind: domain.GeneratePlaylist, Playlist: &domain.PlaylistInput{MaxTracks: tag, MaxDurationSeconds: 60}}
}

func startTestPool(t *testing.T, processor Processor, workers, capacity int) (*Pool, *TaskService) {
	t.Helper()
	tasks := New(storage.NewMemory(), processor)
	pool, err := StartPool(tasks, workers, capacity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Shutdown)
	return pool, tasks
}

func submitTagged(t *testing.T, pool *Pool, tag int) domain.Task {
	t.Helper()
	task, err := pool.CreateAndSubmit(taggedInput(tag))
	if err != nil {
		t.Fatalf("submit tag %d: %v", tag, err)
	}
	return task
}

func shutdownWithin(t *testing.T, pool *Pool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Shutdown did not return, workers are still running")
	}
}

func requireStatus(t *testing.T, tasks *TaskService, id string, want domain.TaskStatus) domain.Task {
	t.Helper()
	task, err := tasks.Get(id)
	if err != nil || task.Status != want {
		t.Fatalf("%s is %s (%v), want %s", id, task.Status, err, want)
	}
	return task
}

func TestStartPoolValidatesConfiguration(t *testing.T) {
	tasks := New(storage.NewMemory(), openGate())
	if _, err := StartPool(tasks, 0, 4); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("zero workers accepted: %v", err)
	}
	if _, err := StartPool(tasks, 2, 0); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("zero capacity accepted: %v", err)
	}
	if _, err := StartPool(nil, 2, 4); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("missing service accepted: %v", err)
	}
	pool, err := StartPool(tasks, 2, 4)
	if err != nil || pool.Workers() != 2 || pool.QueueCapacity() != 4 {
		t.Fatalf("pool %+v, %v", pool, err)
	}
	shutdownWithin(t, pool)
}

func TestSubmitReturnsBeforeProcessingFinishes(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 2, 4)
	task := submitTagged(t, pool, 7)
	if task.Status != domain.StatusQueued {
		t.Fatalf("submitted task is %s, want queued", task.Status)
	}
	processor.waitForStarts(t, 1)
	requireStatus(t, tasks, task.ID, domain.StatusProcessing)
	processor.openGate()
	shutdownWithin(t, pool)
	done := requireStatus(t, tasks, task.ID, domain.StatusCompleted)
	if done.Result.Playlist.SelectedCount != 7 {
		t.Fatalf("result %+v", done.Result.Playlist)
	}
}

func TestPoolProcessesManyTasksWithTheirOwnResults(t *testing.T) {
	pool, tasks := startTestPool(t, openGate(), 4, 40)
	submitted := map[string]int{}
	for tag := 1; tag <= 40; tag++ {
		if tag == failingTag {
			continue
		}
		submitted[submitTagged(t, pool, tag).ID] = tag
	}
	shutdownWithin(t, pool)
	for id, tag := range submitted {
		task := requireStatus(t, tasks, id, domain.StatusCompleted)
		if task.Result.Playlist.SelectedCount != tag {
			t.Errorf("%s holds the result of tag %d, want %d", id, task.Result.Playlist.SelectedCount, tag)
		}
	}
}

func TestTwoTasksAreProcessedAtTheSameTime(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 2, 4)
	first := submitTagged(t, pool, 1)
	second := submitTagged(t, pool, 2)
	processor.waitForStarts(t, 2)
	if processor.inFlight.Load() != 2 {
		t.Fatalf("%d tasks in flight, want 2", processor.inFlight.Load())
	}
	requireStatus(t, tasks, first.ID, domain.StatusProcessing)
	requireStatus(t, tasks, second.ID, domain.StatusProcessing)
	processor.openGate()
	shutdownWithin(t, pool)
}

func TestWorkerCountLimitsConcurrency(t *testing.T) {
	const workers = 3
	processor := newGatedProcessor()
	pool, _ := startTestPool(t, processor, workers, 10)
	for tag := 1; tag <= 10; tag++ {
		submitTagged(t, pool, tag)
	}
	processor.waitForStarts(t, workers)
	if processor.inFlight.Load() != workers {
		t.Fatalf("%d tasks in flight with %d workers", processor.inFlight.Load(), workers)
	}
	processor.openGate()
	shutdownWithin(t, pool)
	if processor.peak.Load() != workers {
		t.Fatalf("peak concurrency %d, want exactly %d", processor.peak.Load(), workers)
	}
	if processor.calls.Load() != 10 {
		t.Fatalf("%d tasks processed, want 10", processor.calls.Load())
	}
}

func TestFailedProcessingMarksTaskFailed(t *testing.T) {
	pool, tasks := startTestPool(t, openGate(), 2, 4)
	task := submitTagged(t, pool, failingTag)
	shutdownWithin(t, pool)
	failed := requireStatus(t, tasks, task.ID, domain.StatusFailed)
	if failed.Result != nil || failed.Error != "tag 13 always fails" {
		t.Fatalf("failed task %+v", failed)
	}
}

func TestDuplicateAndFinishedSubmissionsAreRejected(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 1, 4)
	running := submitTagged(t, pool, 1)
	waiting := submitTagged(t, pool, 2)
	processor.waitForStarts(t, 1)
	if _, err := pool.Submit(running.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("processing task resubmitted: %v", err)
	}
	if _, err := pool.Submit(waiting.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("queued task resubmitted: %v", err)
	}
	if _, err := pool.Submit("task-9999"); !errors.Is(err, domain.ErrTaskNotFound) {
		t.Fatalf("unknown task submitted: %v", err)
	}
	processor.openGate()
	shutdownWithin(t, pool)
	requireStatus(t, tasks, running.ID, domain.StatusCompleted)
	if processor.calls.Load() != 2 {
		t.Fatalf("%d processor calls, want 2 (no duplicate processing)", processor.calls.Load())
	}
}

func TestCompletedTaskCannotBeSubmittedAgain(t *testing.T) {
	processor := openGate()
	tasks := New(storage.NewMemory(), processor)
	first, err := StartPool(tasks, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	task := submitTagged(t, first, 3)
	shutdownWithin(t, first)
	second, err := StartPool(tasks, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Shutdown()
	if _, err := second.Submit(task.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("completed task resubmitted: %v", err)
	}
}

func TestSubmitRejectsWhenQueueFull(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 1, 2)
	submitTagged(t, pool, 1)
	processor.waitForStarts(t, 1)
	submitTagged(t, pool, 2)
	submitTagged(t, pool, 3)
	rejected, err := pool.CreateAndSubmit(taggedInput(4))
	if !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("fourth task accepted into a full queue: %v", err)
	}
	requireStatus(t, tasks, rejected.ID, domain.StatusPending)
	processor.openGate()
	shutdownWithin(t, pool)
	requireStatus(t, tasks, rejected.ID, domain.StatusPending)
	if processor.calls.Load() != 3 {
		t.Fatalf("%d tasks processed, want the 3 accepted ones", processor.calls.Load())
	}
}

func TestTasksCanBeReadWhileOthersProcess(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 2, 8)
	var ids []string
	for tag := 1; tag <= 6; tag++ {
		ids = append(ids, submitTagged(t, pool, tag).ID)
	}
	processor.waitForStarts(t, 2)
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 100 {
				for _, id := range ids {
					if _, err := tasks.Get(id); err != nil {
						t.Error(err)
						return
					}
				}
				if len(tasks.List()) != len(ids) {
					t.Error("list lost tasks during processing")
					return
				}
			}
		}()
	}
	readers.Wait()
	processor.openGate()
	shutdownWithin(t, pool)
	for _, id := range ids {
		requireStatus(t, tasks, id, domain.StatusCompleted)
	}
}

func TestShutdownDrainsAcceptedTasks(t *testing.T) {
	processor := newGatedProcessor()
	pool, tasks := startTestPool(t, processor, 1, 5)
	var ids []string
	for tag := 1; tag <= 5; tag++ {
		ids = append(ids, submitTagged(t, pool, tag).ID)
	}
	processor.waitForStarts(t, 1)
	finished := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(finished)
	}()
	select {
	case <-finished:
		t.Fatal("Shutdown returned while an accepted task was still blocked")
	default:
	}
	processor.openGate()
	select {
	case <-finished:
	case <-time.After(testTimeout):
		t.Fatal("Shutdown did not finish after the gate opened")
	}
	for _, id := range ids {
		requireStatus(t, tasks, id, domain.StatusCompleted)
	}
	if processor.calls.Load() != 5 {
		t.Fatalf("%d tasks processed, want all 5 accepted tasks", processor.calls.Load())
	}
}

func TestSubmitAfterShutdownIsRejected(t *testing.T) {
	pool, tasks := startTestPool(t, openGate(), 2, 4)
	shutdownWithin(t, pool)
	task, err := pool.CreateAndSubmit(taggedInput(1))
	if !errors.Is(err, domain.ErrPoolStopped) {
		t.Fatalf("submission after shutdown: %v", err)
	}
	requireStatus(t, tasks, task.ID, domain.StatusPending)
}

func TestShutdownTwiceIsSafe(t *testing.T) {
	pool, _ := startTestPool(t, openGate(), 2, 4)
	var callers sync.WaitGroup
	for range 3 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			pool.Shutdown()
		}()
	}
	callers.Wait()
	shutdownWithin(t, pool)
}

func TestConcurrentSubmitAndShutdownLoseNoAcceptedTask(t *testing.T) {
	processor := openGate()
	pool, tasks := startTestPool(t, processor, 3, 64)
	var submitters sync.WaitGroup
	var accepted sync.Map
	for worker := range 8 {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			for index := range 20 {
				task, err := pool.CreateAndSubmit(taggedInput(worker*100 + index + 20))
				switch {
				case err == nil:
					accepted.Store(task.ID, true)
				case errors.Is(err, domain.ErrPoolStopped), errors.Is(err, domain.ErrQueueFull):
				default:
					t.Error(err)
				}
			}
		}()
	}
	pool.Shutdown()
	submitters.Wait()
	count := 0
	accepted.Range(func(id, _ any) bool {
		count++
		requireStatus(t, tasks, fmt.Sprint(id), domain.StatusCompleted)
		return true
	})
	if int64(count) != processor.calls.Load() {
		t.Fatalf("%d tasks accepted but %d processed", count, processor.calls.Load())
	}
}
