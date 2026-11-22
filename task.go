package main

import (
	"errors"
	"fmt"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "Pending"
	StatusQueued     TaskStatus = "Queued"
	StatusProcessing TaskStatus = "Processing"
	StatusCompleted  TaskStatus = "Completed"
	StatusFailed     TaskStatus = "Failed"
)

var ErrInvalidState = errors.New("invalid task state")

// allowedTransitions is the single source of truth for the task lifecycle.
//
//	Pending -> Queued      (accepted by the worker pool)
//	Queued  -> Pending     (rollback when the queue turned out to be full)
//	Queued  -> Processing  (a worker picked the task up)
//	Pending -> Processing  (synchronous processing from Checkpoint 1)
//	Processing -> Completed | Failed
//
// Completed and Failed are final: there are no retries.
var allowedTransitions = map[TaskStatus][]TaskStatus{
	StatusPending:    {StatusQueued, StatusProcessing},
	StatusQueued:     {StatusProcessing, StatusPending},
	StatusProcessing: {StatusCompleted, StatusFailed},
}

type Task struct {
	ID     int
	Input  string
	Status TaskStatus
	Result TextResult
	Error  string
}

// transitionTo changes the status only if the move is allowed.
// It is NOT synchronized by itself: the caller (TaskStore) must hold its lock.
func (t *Task) transitionTo(next TaskStatus) error {
	for _, allowed := range allowedTransitions[t.Status] {
		if allowed == next {
			t.Status = next
			return nil
		}
	}
	return fmt.Errorf("%w: task %d is %s, cannot move to %s", ErrInvalidState, t.ID, t.Status, next)
}

// finish stores the outcome of the analysis. The caller must hold the store lock.
func (t *Task) finish(result TextResult, err error) error {
	if err != nil {
		if tErr := t.transitionTo(StatusFailed); tErr != nil {
			return tErr
		}
		t.Error = err.Error()
		return fmt.Errorf("process task %d: %w", t.ID, err)
	}
	if tErr := t.transitionTo(StatusCompleted); tErr != nil {
		return tErr
	}
	t.Result = result
	return nil
}
