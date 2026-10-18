package storage

import (
	"fmt"
	"strings"

	"setlist/internal/domain"
)

// Memory owns task values. Reads and writes detach all nested mutable fields.
type Memory struct {
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
	if _, ok := m.tasks[t.ID]; ok {
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
	t, ok := m.tasks[id]
	if !ok {
		return domain.Task{}, fmt.Errorf("%w: %s", domain.ErrTaskNotFound, id)
	}
	return t.Clone(), nil
}

// Start and Finish are the only mutation operations; no caller can overwrite an
// existing task's identity or input, or bypass its lifecycle through storage.
func (m *Memory) Start(id string) (domain.Task, error) {
	t, err := m.Get(id)
	if err != nil {
		return t, err
	}
	if t.Status != domain.StatusPending {
		return t, fmt.Errorf("%w: %s", domain.ErrAlreadyProcessed, id)
	}
	if err = t.Transition(domain.StatusProcessing); err != nil {
		return t, err
	}
	m.tasks[id] = t.Clone()
	return t, nil
}

func (m *Memory) Finish(id string, result domain.TaskResult, processingErr error) (domain.Task, error) {
	t, err := m.Get(id)
	if err != nil {
		return t, err
	}
	next := domain.StatusCompleted
	if processingErr != nil {
		next = domain.StatusFailed
	}
	if err = t.Transition(next); err != nil {
		return t, err
	}
	if processingErr != nil {
		t.Error = processingErr.Error()
	} else {
		if err = result.Validate(t.Input.Kind); err != nil {
			return domain.Task{}, err
		}
		t.Result = &result
	}
	m.tasks[id] = t.Clone()
	return t.Clone(), nil
}

func (m *Memory) List() []domain.Task {
	out := make([]domain.Task, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.tasks[id].Clone())
	}
	return out
}
