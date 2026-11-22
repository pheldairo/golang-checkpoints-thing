package service

import (
	"fmt"
	"sync"

	"setlist/internal/domain"
)

const (
	DefaultWorkers       = 3
	DefaultQueueCapacity = 16
)

type Pool struct {
	tasks    *TaskService
	queue    chan string
	workers  int
	mu       sync.Mutex
	stopped  bool
	running  sync.WaitGroup
	stopOnce sync.Once
}

func StartPool(tasks *TaskService, workers, queueCapacity int) (*Pool, error) {
	if tasks == nil {
		return nil, fmt.Errorf("%w: worker pool needs a task service", domain.ErrInvalidInput)
	}
	if workers < 1 {
		return nil, fmt.Errorf("%w: worker pool needs at least one worker, got %d", domain.ErrInvalidInput, workers)
	}
	if queueCapacity < 1 {
		return nil, fmt.Errorf("%w: task queue capacity must be at least 1, got %d", domain.ErrInvalidInput, queueCapacity)
	}
	pool := &Pool{
		tasks:   tasks,
		queue:   make(chan string, queueCapacity),
		workers: workers,
	}
	pool.running.Add(workers)
	for range workers {
		go pool.work()
	}
	return pool, nil
}

func (p *Pool) Workers() int {
	return p.workers
}

func (p *Pool) QueueCapacity() int {
	return cap(p.queue)
}

func (p *Pool) Submit(id string) (domain.Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return domain.Task{}, fmt.Errorf("%w: %s was not accepted", domain.ErrPoolStopped, id)
	}
	if len(p.queue) == cap(p.queue) {
		task, err := p.tasks.Get(id)
		if err != nil {
			return domain.Task{}, err
		}
		return task, fmt.Errorf("%w: %s stays %s, capacity is %d", domain.ErrQueueFull, id, task.Status, cap(p.queue))
	}
	task, err := p.tasks.enqueue(id)
	if err != nil {
		return task, err
	}
	p.queue <- id
	return task, nil
}

func (p *Pool) CreateAndSubmit(input domain.TaskInput) (domain.Task, error) {
	task, err := p.tasks.Create(input)
	if err != nil {
		return domain.Task{}, err
	}
	queued, err := p.Submit(task.ID)
	if err != nil {
		return task, err
	}
	return queued, nil
}

func (p *Pool) Shutdown() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		close(p.queue)
		p.mu.Unlock()
		p.running.Wait()
	})
}

func (p *Pool) work() {
	defer p.running.Done()
	for id := range p.queue {
		_, _ = p.tasks.processQueued(id)
	}
}
