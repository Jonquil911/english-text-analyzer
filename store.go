package main

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrTaskNotFound = errors.New("task not found")

// TaskStore keeps all tasks in memory. It is safe for concurrent use:
// every read or write of the map AND of the Task fields inside it
// happens while holding mu.
type TaskStore struct {
	mu        sync.RWMutex // protects tasks, nextID and every *Task in the map
	tasks     map[int]*Task
	nextID    int
	processor TextProcessor // read-only after construction, no lock needed
}

func NewTaskStore(processor TextProcessor) *TaskStore {
	if processor == nil {
		processor = TextAnalyzer{}
	}
	return &TaskStore{
		tasks:     make(map[int]*Task),
		nextID:    1,
		processor: processor,
	}
}

func (s *TaskStore) Create(text string) (Task, error) {
	// Validation does not touch shared state, so it runs before locking.
	if err := ValidateText(text); err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	task := &Task{
		ID:     s.nextID,
		Input:  text,
		Status: StatusPending,
	}
	s.tasks[task.ID] = task
	s.nextID++
	return *task, nil
}

// Get returns a copy, so callers never hold a pointer into shared state.
// RLock lets many readers run at the same time while workers are processing.
func (s *TaskStore) Get(id int) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: id %d", ErrTaskNotFound, id)
	}
	return *task, nil
}

// Process analyzes a Pending task synchronously (Checkpoint 1 behaviour).
func (s *TaskStore) Process(id int) (Task, error) {
	return s.run(id, StatusPending)
}

// processQueued is called by workers for tasks that were accepted by the pool.
func (s *TaskStore) processQueued(id int) (Task, error) {
	return s.run(id, StatusQueued)
}

// run does the processing in three steps:
//  1. under the lock: check state and move to Processing (atomic check-and-set,
//     so two goroutines can never both start the same task);
//  2. WITHOUT the lock: run the (possibly slow) analysis, so other tasks can
//     be read and processed meanwhile;
//  3. under the lock again: save the result into this task only.
func (s *TaskStore) run(id int, from TaskStatus) (Task, error) {
	s.mu.Lock()
	task, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return Task{}, fmt.Errorf("%w: id %d", ErrTaskNotFound, id)
	}
	if task.Status != from {
		snapshot := *task
		s.mu.Unlock()
		return snapshot, fmt.Errorf("%w: task %d is %s", ErrInvalidState, id, snapshot.Status)
	}
	if err := task.transitionTo(StatusProcessing); err != nil {
		snapshot := *task
		s.mu.Unlock()
		return snapshot, err
	}
	input := task.Input // copy what the analysis needs while still locked
	s.mu.Unlock()

	result, analyzeErr := s.processor.Analyze(input)

	s.mu.Lock()
	defer s.mu.Unlock()
	err := task.finish(result, analyzeErr)
	return *task, err
}

// markQueued atomically moves a task from Pending to Queued.
// It is the guard that rejects duplicate and invalid submissions.
func (s *TaskStore) markQueued(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("%w: id %d", ErrTaskNotFound, id)
	}
	if task.Status != StatusPending {
		return fmt.Errorf("%w: task %d is %s, only Pending tasks can be submitted", ErrInvalidState, id, task.Status)
	}
	return task.transitionTo(StatusQueued)
}

// unqueue rolls a task back to Pending when it could not be put into the queue.
func (s *TaskStore) unqueue(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task, ok := s.tasks[id]; ok && task.Status == StatusQueued {
		_ = task.transitionTo(StatusPending) // Queued -> Pending is always allowed
	}
}

func (s *TaskStore) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]int, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	result := make([]Task, 0, len(ids))
	for _, id := range ids {
		result = append(result, *s.tasks[id])
	}
	return result
}
