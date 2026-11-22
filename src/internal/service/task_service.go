package service

import (
	"errors"
	"fmt"
	"sync/atomic"

	"setlist/internal/domain"
	"setlist/internal/storage"
)

type Processor interface {
	Process(domain.TaskInput) (domain.TaskResult, error)
}

type TaskService struct {
	store     *storage.Memory
	processor Processor
	nextID    atomic.Uint64
}

func New(store *storage.Memory, processor Processor) *TaskService {
	return &TaskService{store: store, processor: processor}
}

func (s *TaskService) Create(input domain.TaskInput) (domain.Task, error) {
	if err := input.Validate(); err != nil {
		return domain.Task{}, err
	}
	id := s.unusedID()
	task := domain.Task{ID: id, Input: input, Status: domain.StatusPending}
	if err := s.store.Add(task); err != nil {
		return domain.Task{}, err
	}
	return s.store.Get(id)
}

func (s *TaskService) Get(id string) (domain.Task, error) {
	return s.store.Get(id)
}

func (s *TaskService) List() []domain.Task {
	return s.store.List()
}

func (s *TaskService) Process(id string) (domain.Task, error) {
	task, err := s.store.Start(id)
	if err != nil {
		return task, err
	}
	return s.run(task)
}

func (s *TaskService) enqueue(id string) (domain.Task, error) {
	return s.store.Enqueue(id)
}

func (s *TaskService) processQueued(id string) (domain.Task, error) {
	task, err := s.store.StartQueued(id)
	if err != nil {
		return task, err
	}
	return s.run(task)
}

func (s *TaskService) run(task domain.Task) (domain.Task, error) {
	result, processingErr := s.processor.Process(task.Input)
	if processingErr == nil {
		processingErr = result.Validate(task.Input.Kind)
	}
	final, err := s.store.Finish(task.ID, result, processingErr)
	if err != nil {
		return final, err
	}
	if processingErr != nil {
		return final, fmt.Errorf("process %s: %w", task.ID, processingErr)
	}
	return final, nil
}

func (s *TaskService) unusedID() string {
	for {
		id := fmt.Sprintf("task-%04d", s.nextID.Add(1))
		if _, err := s.store.Get(id); errors.Is(err, domain.ErrTaskNotFound) {
			return id
		}
	}
}
