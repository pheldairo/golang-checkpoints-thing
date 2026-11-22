package storage

import (
	"fmt"
	"strings"
	"sync"

	"setlist/internal/domain"
)

type Memory struct {
	mu    sync.RWMutex
	tasks map[string]domain.Task
	order []string
}

func NewMemory() *Memory {
	return &Memory{tasks: make(map[string]domain.Task)}
}

func (m *Memory) Add(t domain.Task) error {
	if strings.TrimSpace(t.ID) == "" || t.Status != domain.StatusPending || t.Result != nil || t.Error != "" {
		return fmt.Errorf("%w: new task must be pending with an ID", domain.ErrInvalidInput)
	}
	if err := t.Input.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[t.ID]; exists {
		return fmt.Errorf("%w: %s", domain.ErrDuplicateTask, t.ID)
	}
	if m.tasks == nil {
		m.tasks = make(map[string]domain.Task)
	}
	m.tasks[t.ID] = t.Clone()
	m.order = append(m.order, t.ID)
	return nil
}

func (m *Memory) Get(id string) (domain.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, err := m.find(id)
	if err != nil {
		return domain.Task{}, err
	}
	return task.Clone(), nil
}

func (m *Memory) List() []domain.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tasks := make([]domain.Task, 0, len(m.order))
	for _, id := range m.order {
		tasks = append(tasks, m.tasks[id].Clone())
	}
	return tasks
}

func (m *Memory) Enqueue(id string) (domain.Task, error) {
	return m.move(id, domain.StatusPending, domain.StatusQueued, domain.ErrInvalidState)
}

func (m *Memory) Start(id string) (domain.Task, error) {
	return m.move(id, domain.StatusPending, domain.StatusProcessing, domain.ErrAlreadyProcessed)
}

func (m *Memory) StartQueued(id string) (domain.Task, error) {
	return m.move(id, domain.StatusQueued, domain.StatusProcessing, domain.ErrInvalidState)
}

func (m *Memory) Finish(id string, result domain.TaskResult, processingErr error) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.find(id)
	if err != nil {
		return domain.Task{}, err
	}
	next := domain.StatusCompleted
	if processingErr != nil {
		next = domain.StatusFailed
	}
	if err = task.Transition(next); err != nil {
		return task.Clone(), err
	}
	if processingErr != nil {
		task.Error = processingErr.Error()
	} else {
		if err = result.Validate(task.Input.Kind); err != nil {
			return domain.Task{}, err
		}
		stored := result.Clone()
		task.Result = &stored
	}
	m.tasks[id] = task
	return task.Clone(), nil
}

func (m *Memory) move(id string, required, next domain.TaskStatus, wrongState error) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.find(id)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Status == domain.StatusQueued && required != domain.StatusQueued {
		return task.Clone(), fmt.Errorf("%w: %s is already queued for a worker", domain.ErrInvalidState, id)
	}
	if task.Status != required {
		return task.Clone(), fmt.Errorf("%w: %s is %s, needs %s", wrongState, id, task.Status, required)
	}
	if err = task.Transition(next); err != nil {
		return task.Clone(), err
	}
	m.tasks[id] = task
	return task.Clone(), nil
}

func (m *Memory) find(id string) (domain.Task, error) {
	task, ok := m.tasks[id]
	if !ok {
		return domain.Task{}, fmt.Errorf("%w: %s", domain.ErrTaskNotFound, id)
	}
	return task, nil
}
