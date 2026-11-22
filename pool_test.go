package main

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// failsafe is only a guard against a hanging test (deadlock / lost signal).
// It is never used to "wait until work is probably done".
const failsafe = 2 * time.Second

// blockingProcessor lets a test control exactly when analysis finishes.
// Each Analyze call reports "started" and then waits for `release` to be closed.
type blockingProcessor struct {
	started chan string
	release chan struct{}
}

func newBlockingProcessor() *blockingProcessor {
	return &blockingProcessor{
		started: make(chan string, 100),
		release: make(chan struct{}),
	}
}

func (p *blockingProcessor) Analyze(text string) (TextResult, error) {
	p.started <- text
	<-p.release
	return TextAnalyzer{}.Analyze(text)
}

// waitStarted waits until n analyses are running at the same time.
func (p *blockingProcessor) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-p.started:
		case <-time.After(failsafe):
			t.Fatalf("only %d of %d tasks started processing", i, n)
		}
	}
}

func newPool(t *testing.T, store *TaskStore, workers, queueSize int) *WorkerPool {
	t.Helper()
	pool, err := NewWorkerPool(store, workers, queueSize)
	if err != nil {
		t.Fatalf("NewWorkerPool: %v", err)
	}
	return pool
}

func mustCreate(t *testing.T, store *TaskStore, text string) Task {
	t.Helper()
	task, err := store.Create(text)
	if err != nil {
		t.Fatalf("Create(%q): %v", text, err)
	}
	return task
}

// shutdownWithin fails the test if Shutdown hangs (e.g. a deadlock).
func shutdownWithin(t *testing.T, pool *WorkerPool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(failsafe):
		t.Fatal("Shutdown did not return: workers did not terminate")
	}
}

func TestNewWorkerPoolValidation(t *testing.T) {
	store := NewTaskStore(nil)
	cases := []struct{ workers, queue int }{{0, 5}, {-1, 5}, {2, 0}}
	for _, c := range cases {
		if _, err := NewWorkerPool(store, c.workers, c.queue); err == nil {
			t.Errorf("NewWorkerPool(%d, %d) should fail", c.workers, c.queue)
		}
	}
	if _, err := NewWorkerPool(nil, 2, 2); err == nil {
		t.Error("NewWorkerPool with nil store should fail")
	}
}

func TestWorkerPoolStartup(t *testing.T) {
	pool := newPool(t, NewTaskStore(nil), 3, 5)
	if pool.Workers() != 3 || pool.QueueCapacity() != 5 {
		t.Fatalf("workers=%d capacity=%d; want 3 and 5", pool.Workers(), pool.QueueCapacity())
	}
	if got := pool.RunningWorkers(); got != 3 {
		t.Fatalf("RunningWorkers = %d; want 3", got)
	}
	shutdownWithin(t, pool)
}

// Submission must return while the task is still unfinished.
func TestSubmitIsAsynchronous(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 5)

	task := mustCreate(t, store, "hello async world")
	if err := pool.Submit(task.ID); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// We got here although Analyze is blocked => Submit did not wait.
	got, _ := store.Get(task.ID)
	if got.Status != StatusQueued && got.Status != StatusProcessing {
		t.Fatalf("status right after Submit = %s; want Queued or Processing", got.Status)
	}

	proc.waitStarted(t, 1)
	got, _ = store.Get(task.ID)
	if got.Status != StatusProcessing {
		t.Fatalf("status while analyzing = %s; want Processing", got.Status)
	}

	close(proc.release)
	shutdownWithin(t, pool)
	got, _ = store.Get(task.ID)
	if got.Status != StatusCompleted {
		t.Fatalf("final status = %s; want Completed", got.Status)
	}
}

// Proves real parallelism: both analyses are running at the same moment,
// because neither can finish until both have started.
func TestTasksOverlapInProcessing(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 5)

	a := mustCreate(t, store, "first task")
	b := mustCreate(t, store, "second task")
	for _, id := range []int{a.ID, b.ID} {
		if err := pool.Submit(id); err != nil {
			t.Fatalf("Submit(%d): %v", id, err)
		}
	}

	proc.waitStarted(t, 2) // both are inside Analyze simultaneously

	for _, id := range []int{a.ID, b.ID} {
		got, _ := store.Get(id) // reading while other tasks are processing
		if got.Status != StatusProcessing {
			t.Errorf("task %d status = %s; want Processing", id, got.Status)
		}
	}

	close(proc.release)
	shutdownWithin(t, pool)
}

// Many tasks, each result must belong to its own task. Order is not assumed.
func TestManyTasksCorrectResultsAndStates(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	const n = 50
	pool := newPool(t, store, 4, n)

	want := make(map[int]TextResult)
	for i := 0; i < n; i++ {
		text := fmt.Sprintf("task%d %s", i, repeatWord("w", i%5+1))
		task := mustCreate(t, store, text)
		expected, _ := TextAnalyzer{}.Analyze(text)
		want[task.ID] = expected
		if err := pool.Submit(task.ID); err != nil {
			t.Fatalf("Submit(%d): %v", task.ID, err)
		}
	}

	shutdownWithin(t, pool) // waits for all accepted tasks

	for id, expected := range want {
		got, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusCompleted {
			t.Errorf("task %d status = %s; want Completed", id, got.Status)
		}
		if got.Result != expected {
			t.Errorf("task %d result = %+v; want %+v", id, got.Result, expected)
		}
	}
}

func repeatWord(word string, n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += word + " "
	}
	return s
}

func TestSubmitInvalidAndDuplicate(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 5)

	if err := pool.Submit(999); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("Submit(missing) = %v; want ErrTaskNotFound", err)
	}

	task := mustCreate(t, store, "duplicate check")
	if err := pool.Submit(task.ID); err != nil {
		t.Fatal(err)
	}
	// Second submit while Queued/Processing must fail.
	if err := pool.Submit(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("duplicate Submit = %v; want ErrInvalidState", err)
	}
	proc.waitStarted(t, 1)
	if err := pool.Submit(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Submit of Processing task = %v; want ErrInvalidState", err)
	}
	// Sync processing of a task owned by the pool must also be refused.
	if _, err := store.Process(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("store.Process of Processing task = %v; want ErrInvalidState", err)
	}

	close(proc.release)
	shutdownWithin(t, pool)

	got, _ := store.Get(task.ID)
	if got.Status != StatusCompleted {
		t.Fatalf("status = %s; want Completed (processed exactly once)", got.Status)
	}
	if len(proc.started) != 0 {
		t.Fatalf("task was analyzed more than once")
	}
}

func TestSubmitCompletedTaskRejected(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	pool := newPool(t, store, 2, 5)
	defer pool.Shutdown()

	task := mustCreate(t, store, "already done")
	if _, err := store.Process(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.Submit(task.ID); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Submit(completed) = %v; want ErrInvalidState", err)
	}
}

func TestFailedProcessingInPool(t *testing.T) {
	store := NewTaskStore(failingProcessor{})
	pool := newPool(t, store, 2, 5)

	task := mustCreate(t, store, "will fail")
	if err := pool.Submit(task.ID); err != nil {
		t.Fatal(err)
	}
	shutdownWithin(t, pool)

	got, _ := store.Get(task.ID)
	if got.Status != StatusFailed || got.Error != "simulated processing error" {
		t.Fatalf("got %+v; want Failed with error message", got)
	}
	if got.Result != (TextResult{}) {
		t.Fatal("failed task must not have a result")
	}
}

// 2 workers busy + queue capacity 1 => 3rd task waits in queue, 4th is rejected.
func TestFullQueueRejectsAndKeepsTaskPending(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 1)

	var ids []int
	for i := 0; i < 4; i++ {
		ids = append(ids, mustCreate(t, store, fmt.Sprintf("text %d", i)).ID)
	}
	// Submit one by one and wait until a worker takes it, so the queue is empty again.
	for _, id := range ids[:2] {
		if err := pool.Submit(id); err != nil {
			t.Fatal(err)
		}
		proc.waitStarted(t, 1)
	}
	// Now both workers are busy inside Analyze and the queue is empty.

	if err := pool.Submit(ids[2]); err != nil {
		t.Fatalf("third Submit should fit in the queue: %v", err)
	}
	if err := pool.Submit(ids[3]); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("fourth Submit = %v; want ErrQueueFull", err)
	}
	rejected, _ := store.Get(ids[3])
	if rejected.Status != StatusPending {
		t.Fatalf("rejected task status = %s; want Pending", rejected.Status)
	}

	close(proc.release)
	shutdownWithin(t, pool)

	for _, id := range ids[:3] {
		got, _ := store.Get(id)
		if got.Status != StatusCompleted {
			t.Errorf("accepted task %d status = %s; want Completed", id, got.Status)
		}
	}
}

func TestRejectedTaskCanBeSubmittedLater(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 1)

	a := mustCreate(t, store, "a")
	b := mustCreate(t, store, "b")
	c := mustCreate(t, store, "c")
	d := mustCreate(t, store, "d")
	for _, id := range []int{a.ID, b.ID} {
		if err := pool.Submit(id); err != nil {
			t.Fatal(err)
		}
		proc.waitStarted(t, 1)
	}
	if err := pool.Submit(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.Submit(d.ID); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}

	close(proc.release)
	proc.waitStarted(t, 1) // c left the queue and is being processed
	if err := pool.Submit(d.ID); err != nil {
		t.Fatalf("resubmit after space freed: %v", err)
	}
	shutdownWithin(t, pool)

	got, _ := store.Get(d.ID)
	if got.Status != StatusCompleted {
		t.Fatalf("resubmitted task status = %s; want Completed", got.Status)
	}
}

// Run with -race: many goroutines create, submit, read and list at once.
func TestConcurrentAccessToStore(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	const goroutines, perG = 10, 20
	pool := newPool(t, store, 4, goroutines*perG)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perG)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				task, err := store.Create(fmt.Sprintf("goroutine %d text %d", g, i))
				if err != nil {
					errs <- err
					return
				}
				if err := pool.Submit(task.ID); err != nil {
					errs <- err
					return
				}
				if _, err := store.Get(task.ID); err != nil {
					errs <- err
				}
				_ = store.List()
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	shutdownWithin(t, pool)

	tasks := store.List()
	if len(tasks) != goroutines*perG {
		t.Fatalf("stored %d tasks; want %d", len(tasks), goroutines*perG)
	}
	seen := make(map[int]bool)
	for _, task := range tasks {
		if seen[task.ID] {
			t.Fatalf("duplicate ID %d", task.ID)
		}
		seen[task.ID] = true
		if task.Status != StatusCompleted {
			t.Errorf("task %d status = %s; want Completed", task.ID, task.Status)
		}
	}
}

// Shutdown must wait for tasks that were accepted (queued or running).
func TestShutdownFinishesAcceptedTasks(t *testing.T) {
	proc := newBlockingProcessor()
	store := NewTaskStore(proc)
	pool := newPool(t, store, 2, 5)

	var ids []int
	for i := 0; i < 5; i++ {
		task := mustCreate(t, store, fmt.Sprintf("job %d", i))
		if err := pool.Submit(task.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	proc.waitStarted(t, 2)

	done := make(chan struct{})
	go func() {
		pool.Shutdown()
		close(done)
	}()

	// Shutdown cannot finish while tasks are still blocked.
	select {
	case <-done:
		t.Fatal("Shutdown returned before accepted tasks were processed")
	default:
	}

	close(proc.release)
	select {
	case <-done:
	case <-time.After(failsafe):
		t.Fatal("Shutdown hangs")
	}

	if got := pool.RunningWorkers(); got != 0 {
		t.Fatalf("RunningWorkers after Shutdown = %d; want 0", got)
	}
	for _, id := range ids {
		got, _ := store.Get(id)
		if got.Status != StatusCompleted {
			t.Errorf("task %d status = %s; want Completed (not lost)", id, got.Status)
		}
	}
}

func TestSubmitAfterShutdownRejected(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	pool := newPool(t, store, 2, 5)
	shutdownWithin(t, pool)

	task := mustCreate(t, store, "too late")
	if err := pool.Submit(task.ID); !errors.Is(err, ErrPoolStopped) {
		t.Fatalf("Submit after Shutdown = %v; want ErrPoolStopped", err)
	}
	got, _ := store.Get(task.ID)
	if got.Status != StatusPending {
		t.Fatalf("rejected task status = %s; want Pending", got.Status)
	}
}

func TestShutdownTwiceIsSafe(t *testing.T) {
	pool := newPool(t, NewTaskStore(nil), 2, 5)
	shutdownWithin(t, pool)
	shutdownWithin(t, pool) // must not panic on double close
	if got := pool.RunningWorkers(); got != 0 {
		t.Fatalf("RunningWorkers = %d; want 0", got)
	}
}

// Submit and Shutdown race each other. No panic allowed, and every task is
// either fully processed (Submit returned nil) or untouched (Submit failed).
func TestConcurrentSubmitAndShutdown(t *testing.T) {
	store := NewTaskStore(TextAnalyzer{})
	const n = 200
	pool := newPool(t, store, 3, n)

	ids := make([]int, n)
	for i := range ids {
		ids[i] = mustCreate(t, store, fmt.Sprintf("race %d", i)).ID
	}

	accepted := make([]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := pool.Submit(ids[i])
			if err != nil && !errors.Is(err, ErrPoolStopped) {
				t.Errorf("unexpected Submit error: %v", err)
			}
			accepted[i] = err == nil // each goroutine writes its own index
		}(i)
	}
	wg.Add(2)
	go func() { defer wg.Done(); pool.Shutdown() }()
	go func() { defer wg.Done(); pool.Shutdown() }()
	wg.Wait()

	for i, id := range ids {
		got, _ := store.Get(id)
		if accepted[i] && got.Status != StatusCompleted {
			t.Errorf("accepted task %d lost: status %s", id, got.Status)
		}
		if !accepted[i] && got.Status != StatusPending {
			t.Errorf("rejected task %d status = %s; want Pending", id, got.Status)
		}
	}
}
