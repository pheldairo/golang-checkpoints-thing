package domain

import "errors"

var (
	ErrInvalidTrack      = errors.New("invalid track")
	ErrTrackNotFound     = errors.New("track not found")
	ErrDuplicateTrack    = errors.New("duplicate track ID")
	ErrInvalidResult     = errors.New("invalid processor result")
	ErrInvalidInput      = errors.New("invalid task input")
	ErrTaskNotFound      = errors.New("task not found")
	ErrDuplicateTask     = errors.New("duplicate task ID")
	ErrInvalidTransition = errors.New("invalid task transition")
	ErrAlreadyProcessed  = errors.New("task already processed")
	ErrInvalidState      = errors.New("task is not in a state that allows this operation")
	ErrQueueFull         = errors.New("task queue is full")
	ErrPoolStopped       = errors.New("worker pool is stopped")
	ErrPlayerUnavailable = errors.New("no player is configured")
)
