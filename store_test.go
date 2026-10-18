package main

import (
	"errors"
	"testing"
)

func TestCreateAndGet(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	task, err := store.Create("Hello world")
	if err != nil {
		t.Fatalf("Create unexpected error: %v", err)
	}
	if task.ID != 1 || task.Status != StatusPending || task.Input != "Hello world" {
		t.Fatalf("Create returned wrong task: %+v", task)
	}
	if task.Result != (TextResult{}) {
		t.Fatal("unprocessed task must not have a result")
	}

	got, err := store.Get(task.ID)
	if err != nil || got != task {
		t.Fatalf("Get(%d) = %+v, %v; want %+v", task.ID, got, err, task)
	}
}

func TestCreateRejectsInvalidInputWithoutConsumingID(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	if _, err := store.Create("   "); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("Create invalid input: got %v, want ErrInvalidText", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("invalid task was stored")
	}
	task, err := store.Create("Valid text")
	if err != nil || task.ID != 1 {
		t.Fatalf("first valid task = %+v, %v; expected ID 1", task, err)
	}
}

func TestMissingTask(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	if _, err := store.Get(99); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("Get(99) error = %v; want ErrTaskNotFound", err)
	}
	if _, err := store.Process(99); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("Process(99) error = %v; want ErrTaskNotFound", err)
	}
}

func TestProcessResultAndInvalidTransition(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	created, err := store.Create("Go go GO!")
	if err != nil {
		t.Fatal(err)
	}
	processed, err := store.Process(created.ID)
	if err != nil {
		t.Fatalf("Process unexpected error: %v", err)
	}
	if processed.Status != StatusCompleted {
		t.Errorf("status = %s; want Completed", processed.Status)
	}
	want := TextResult{WordCount: 3, CharacterCount: 9, UniqueWords: 1, MostFrequentWord: "go", MostFrequentCount: 3}
	if processed.Result != want {
		t.Errorf("result = %+v; want %+v", processed.Result, want)
	}
	stored, err := store.Get(created.ID)
	if err != nil || stored != processed {
		t.Errorf("stored result = %+v, %v; want %+v", stored, err, processed)
	}
	if _, err := store.Process(created.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("processing completed task = %v; want ErrInvalidState", err)
	}
}

func TestMultipleIndependentTasks(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	first, _ := store.Create("red red blue")
	second, _ := store.Create("green yellow")
	third, _ := store.Create("hello")
	if first.ID == second.ID || first.ID == third.ID || second.ID == third.ID {
		t.Fatal("task IDs are not unique")
	}

	processedFirst, err := store.Process(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	processedSecond, err := store.Process(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedThird, err := store.Get(third.ID)
	if err != nil {
		t.Fatal(err)
	}

	if processedFirst.Result.MostFrequentWord != "red" || processedFirst.Result.WordCount != 3 {
		t.Errorf("first task was corrupted: %+v", processedFirst.Result)
	}
	if processedSecond.Result.MostFrequentWord != "green" || processedSecond.Result.WordCount != 2 {
		t.Errorf("second task was corrupted: %+v", processedSecond.Result)
	}
	if storedThird.Status != StatusPending || storedThird.Result != (TextResult{}) {
		t.Errorf("third task should be untouched: %+v", storedThird)
	}

	list := store.List()
	if len(list) != 3 || list[0].ID != first.ID || list[1].ID != second.ID || list[2].ID != third.ID {
		t.Errorf("unexpected task list: %+v", list)
	}
}

type observingProcessor struct {
	onAnalyze func()
}

func (p observingProcessor) Analyze(text string) (TextResult, error) {
	p.onAnalyze()
	return (TextAnalyzer{}).Analyze(text)
}

func TestProcessingStateIsVisibleDuringAnalysis(t *testing.T) {
	var store *TaskStore
	var taskID int
	wasProcessing := false
	processor := observingProcessor{onAnalyze: func() {
		task, err := store.Get(taskID)
		if err != nil {
			t.Errorf("Get during analysis: %v", err)
			return
		}
		wasProcessing = task.Status == StatusProcessing
	}}
	store = NewTaskStore(processor)
	created, err := store.Create("Hello Go")
	if err != nil {
		t.Fatal(err)
	}
	taskID = created.ID
	if _, err := store.Process(taskID); err != nil {
		t.Fatal(err)
	}
	if !wasProcessing {
		t.Fatal("task did not enter Processing while analyzer was running")
	}
}

type failingProcessor struct{}

func (failingProcessor) Analyze(string) (TextResult, error) {
	return TextResult{}, errors.New("simulated processing error")
}

func TestProcessingFailure(t *testing.T) {
	store := NewTaskStore(failingProcessor{})
	created, err := store.Create("Hello")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := store.Process(created.ID)
	if err == nil {
		t.Fatal("expected processing error")
	}
	if failed.Status != StatusFailed || failed.Error != "simulated processing error" {
		t.Fatalf("incorrect failed state: %+v", failed)
	}
	stored, err := store.Get(created.ID)
	if err != nil || stored != failed {
		t.Fatalf("failed task not stored correctly: %+v, %v", stored, err)
	}
	if _, err := store.Process(created.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("retrying failed task = %v; want ErrInvalidState", err)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	created, err := store.Create("Hello")
	if err != nil {
		t.Fatal(err)
	}
	copy, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	copy.Input = "Changed!"
	copy.Status = StatusCompleted
	stored, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Input != "Hello" || stored.Status != StatusPending {
		t.Fatalf("external mutation changed stored task: %+v", stored)
	}
}
