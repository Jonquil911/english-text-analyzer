package main

import (
	"errors"
	"fmt"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "Pending"
	StatusProcessing TaskStatus = "Processing"
	StatusCompleted  TaskStatus = "Completed"
	StatusFailed     TaskStatus = "Failed"
)

var ErrInvalidState = errors.New("invalid task state")

type Task struct {
	ID     int
	Input  string
	Status TaskStatus
	Result TextResult
	Error  string
}

func (t *Task) Process(processor TextProcessor) error {
	if t.Status != StatusPending {
		return fmt.Errorf("%w: task %d is %s", ErrInvalidState, t.ID, t.Status)
	}
	if processor == nil {
		return errors.New("text processor is nil")
	}

	t.Status = StatusProcessing
	result, err := processor.Analyze(t.Input)
	if err != nil {
		t.Status = StatusFailed
		t.Error = err.Error()
		return fmt.Errorf("process task %d: %w", t.ID, err)
	}

	t.Result = result
	t.Status = StatusCompleted
	return nil
}
