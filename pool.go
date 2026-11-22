package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var (
	ErrQueueFull   = errors.New("task queue is full")
	ErrPoolStopped = errors.New("worker pool is stopped")
)

// WorkerPool processes tasks asynchronously with a FIXED number of workers
// that read task IDs from a BOUNDED buffered channel.
//
// Channel ownership: the pool owns `queue`. Only Submit sends to it and only
// Shutdown closes it, and both do so while holding mu, so a send can never
// happen on a closed channel.
//
// Lock order: pool.mu -> store.mu. Workers only take store.mu, never pool.mu,
// so there is no cycle and no deadlock.
type WorkerPool struct {
	store   *TaskStore
	queue   chan int // bounded queue of task IDs waiting for a worker
	workers int

	mu     sync.Mutex // protects closed and serializes sends with close(queue)
	closed bool

	wg      sync.WaitGroup // counts running worker goroutines
	running atomic.Int32   // number of live workers (for tests and defense)
}

// NewWorkerPool starts `workers` goroutines immediately.
// queueSize is the maximum number of tasks waiting in the queue.
func NewWorkerPool(store *TaskStore, workers, queueSize int) (*WorkerPool, error) {
	if store == nil {
		return nil, errors.New("store is nil")
	}
	if workers < 1 {
		return nil, fmt.Errorf("workers must be >= 1, got %d", workers)
	}
	if queueSize < 1 {
		return nil, fmt.Errorf("queue size must be >= 1, got %d", queueSize)
	}

	p := &WorkerPool{
		store:   store,
		queue:   make(chan int, queueSize),
		workers: workers,
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1) // Add BEFORE starting the goroutine, so Wait cannot miss it
		p.running.Add(1)
		go p.worker()
	}
	return p, nil
}

// worker is a long-lived goroutine: it handles many tasks, one at a time.
// `range` stops only after the queue is closed AND drained, so every task
// accepted before Shutdown is still processed.
func (p *WorkerPool) worker() {
	defer p.wg.Done()
	defer p.running.Add(-1)

	for id := range p.queue {
		// The error is already saved in the task (Status=Failed, Error=...),
		// so the worker just moves on to the next task.
		_, _ = p.store.processQueued(id)
	}
}

// Submit puts a Pending task into the queue and returns immediately;
// it never waits for processing.
//
// Full queue policy (Option A): the submission is rejected with ErrQueueFull,
// and the task is returned to Pending, so it can be submitted again later.
func (p *WorkerPool) Submit(id int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPoolStopped
	}

	// Pending -> Queued, atomically. Rejects unknown IDs, duplicates
	// (task already Queued/Processing) and finished tasks.
	if err := p.store.markQueued(id); err != nil {
		return err
	}

	// Non-blocking send: we hold mu here, so we must never block.
	select {
	case p.queue <- id:
		return nil
	default:
		p.store.unqueue(id)
		return fmt.Errorf("%w: capacity %d", ErrQueueFull, cap(p.queue))
	}
}

// Shutdown stops accepting new tasks, lets workers finish everything that was
// already accepted, and waits until all workers have exited.
// It is safe to call more than once and from several goroutines.
func (p *WorkerPool) Shutdown() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue) // workers' `range` loops end after the queue is drained
	}
	p.mu.Unlock() // unlock BEFORE waiting, otherwise Submit callers would block

	p.wg.Wait()
}

func (p *WorkerPool) Workers() int       { return p.workers }
func (p *WorkerPool) QueueCapacity() int { return cap(p.queue) }
func (p *WorkerPool) RunningWorkers() int {
	return int(p.running.Load())
}
