package service

import (
	"errors"
	"fmt"

	"setlist/internal/domain"
	"setlist/internal/storage"
)

// Processor allows lifecycle tests and future delivery layers to reuse the service.
type Processor interface {
	Process(domain.TaskInput) (domain.TaskResult, error)
}

type TaskService struct {
	store     *storage.Memory
	processor Processor
	nextID    uint64
}

func New(store *storage.Memory, processor Processor) *TaskService {
	return &TaskService{store: store, processor: processor}
}

func (s *TaskService) Create(input domain.TaskInput) (domain.Task, error) {
	if err := input.Validate(); err != nil {
		return domain.Task{}, err
	}
	var id string
	for {
		s.nextID++
		id = fmt.Sprintf("task-%04d", s.nextID)
		if _, err := s.store.Get(id); errors.Is(err, domain.ErrTaskNotFound) {
			break
		}
	}
	t := domain.Task{ID: id, Input: input, Status: domain.StatusPending}
	if err := s.store.Add(t); err != nil {
		return domain.Task{}, err
	}
	return s.store.Get(id)
}

func (s *TaskService) Get(id string) (domain.Task, error) {
	return s.store.Get(id)
}

func (s *TaskService) Process(id string) (domain.Task, error) {
	t, err := s.store.Start(id)
	if err != nil {
		return t, err
	}
	result, processingErr := s.processor.Process(t.Input)
	if processingErr == nil {
		processingErr = result.Validate(t.Input.Kind)
	}
	final, err := s.store.Finish(id, result, processingErr)
	if err != nil {
		return final, err
	}
	if processingErr != nil {
		return final, fmt.Errorf("process %s: %w", id, processingErr)
	}
	return final, nil
}
