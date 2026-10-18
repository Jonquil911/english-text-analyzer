package main

import (
	"errors"
	"fmt"
	"sort"
)

var ErrTaskNotFound = errors.New("task not found")

type TaskStore struct {
	tasks     map[int]*Task
	nextID    int
	processor TextProcessor
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
	if err := ValidateText(text); err != nil {
		return Task{}, err
	}

	task := &Task{
		ID:     s.nextID,
		Input:  text,
		Status: StatusPending,
	}
	s.tasks[task.ID] = task
	s.nextID++
	return *task, nil
}

func (s *TaskStore) Get(id int) (Task, error) {
	task, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: id %d", ErrTaskNotFound, id)
	}
	return *task, nil
}

func (s *TaskStore) Process(id int) (Task, error) {
	task, ok := s.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: id %d", ErrTaskNotFound, id)
	}

	err := task.Process(s.processor)
	return *task, err
}

func (s *TaskStore) List() []Task {
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
