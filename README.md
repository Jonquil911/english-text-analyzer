# English Text Analyzer — SIS Checkpoint 2

An in-memory Go application (**Go 1.27**, standard library only) that analyzes English text.
Checkpoint 1 built the domain: tasks, validation, the analyzer and the store.
Checkpoint 2 adds **concurrent processing**: tasks are submitted to a **bounded queue**
and processed by a **fixed pool of worker goroutines**.

## 1. Purpose and processing logic

Each task holds an input text. Processing (`TextAnalyzer.Analyze`) computes:

- word count, Unicode character count (runes), unique words;
- the most frequent word (case-insensitive, ties go to the word that appears first).

The analysis logic is unchanged from Checkpoint 1.

## 2. Creating and submitting tasks

1. `store.Create(text)` validates the text and stores a new task with status **Pending**.
   Invalid text (blank or punctuation only) is rejected and gets no ID.
2. `pool.Submit(id)` moves the task to **Queued** and puts its ID into the queue.
   `Submit` **returns immediately** and does not wait for processing.
3. A worker takes the ID from the queue and processes the task.
4. `store.Get(id)` / `store.List()` can be called at any time, even while workers are processing.

Synchronous processing from Checkpoint 1 (`store.Process(id)`) still works for Pending tasks.

## 3. Worker pool

| Setting | Value | Where |
|---|---|---|
| Workers | **3** (fixed) | `workerCount` in `main.go` |
| Queue capacity | **5** task IDs | `queueCapacity` in `main.go` |

- `NewWorkerPool(store, workers, queueSize)` creates a buffered channel `chan int`
  of size `queueSize` and starts exactly `workers` goroutines.
- Each worker is long-lived: `for id := range queue { ... }` — it processes many tasks one after another.
- No goroutine is created per task; the number of goroutines never grows.
- Completion order is not guaranteed.

```text
 Create ──► Pending ──Submit──► [ bounded queue: chan int (cap 5) ] ──► worker 1 ┐
                                                                    ──► worker 2 ├──► TaskStore (mutex)
                                                                    ──► worker 3 ┘
```

## 4. Full queue behaviour (Option A: reject)

If the queue is full, `Submit` returns **`ErrQueueFull`** right away.
The task goes back to **Pending** (it is never marked Processing), so it can be submitted again later.
The send uses `select { case queue <- id: ... default: ... }`, so `Submit` never blocks.

## 5. Task lifecycle

```text
                 Submit              worker picks it up
   Pending ───────────────► Queued ─────────────────────► Processing ──► Completed
      ▲                       │                               ▲    │
      └──── queue full ───────┘                               │    └───► Failed
      │                                                       │
      └──────────── store.Process (sync, Checkpoint 1) ───────┘
```

| From | Allowed to |
|---|---|
| Pending | Queued, Processing (sync) |
| Queued | Processing, Pending (only rollback on full queue) |
| Processing | Completed, Failed |
| Completed / Failed | — (final, no retries) |

Transitions are defined once in `allowedTransitions` (`task.go`).
Invalid operations return `ErrInvalidState`: submitting a task that is already Queued,
Processing, Completed or Failed; processing the same task twice.
Unknown IDs return `ErrTaskNotFound`.

## 6. Synchronization of shared data

**Shared data:** the `tasks` map, `nextID`, and every `*Task` inside the map.

- `TaskStore.mu` (`sync.RWMutex`) protects the map **and the fields of the tasks in it**.
  Writes use `Lock`, reads (`Get`, `List`) use `RLock`, so many readers can work in parallel.
- `Get` and `List` return **copies** of tasks, never pointers into shared state.
- **Check-and-set is atomic:** "is the task Pending?" and "set it to Queued" happen
  under one lock (`markQueued`). The same for "is it Queued?" → "Processing". This is why two
  workers can never process the same task and a duplicate `Submit` is rejected.
- **The lock is not held during analysis.** A worker locks → sets Processing and copies the input →
  unlocks → runs `Analyze` → locks → saves the result into *its own* task → unlocks.
- `WorkerPool.mu` (`sync.Mutex`) protects the `closed` flag and makes "check closed + send"
  atomic with respect to `close(queue)`.
- **Lock order** is always `pool.mu → store.mu`. Workers only take `store.mu`, so there is no cycle → no deadlock.
- `sync.WaitGroup` counts workers; `atomic.Int32` counts running workers (used in tests).

**Channel ownership:** the pool owns `queue`. Only `Submit` sends, only `Shutdown` closes,
both under `pool.mu`, so a send on a closed channel (panic) is impossible.

## 7. Stopping workers

`pool.Shutdown()`:

1. locks `pool.mu`, sets `closed = true`, calls `close(queue)` (only the first time);
2. unlocks — new `Submit` calls now get **`ErrPoolStopped`**;
3. workers keep reading: `range` over a closed channel still returns the buffered IDs,
   so **every accepted task is processed**; then the loop ends and the worker exits;
4. `wg.Wait()` waits for all workers to exit.

Calling `Shutdown` again (or from several goroutines) is safe: it does not close twice and just waits.
The console app calls `Shutdown` with `defer` when you exit.

Not in scope (Checkpoint 3): `context` cancellation, interrupting running tasks, HTTP.

## Project files

| File | Responsibility |
|------|----------------|
| `main.go` | Console menu; creates the store and the worker pool, shuts the pool down on exit |
| `analyzer.go` | Validation, `TextResult`, `TextProcessor` interface and the text analyzer |
| `task.go` | `Task`, statuses, allowed transitions |
| `store.go` | Thread-safe in-memory storage and the processing steps |
| `pool.go` | Worker pool: bounded queue, workers, `Submit`, `Shutdown` |
| `analyzer_test.go` | Table-driven tests for analysis and validation (Checkpoint 1) |
| `store_test.go` | Store and lifecycle tests (Checkpoint 1) |
| `pool_test.go` | Concurrency tests (Checkpoint 2) |

## 8. Run

```bash
go run .
```

```text
1 - Create task
2 - Submit task to worker pool (async)
3 - Process task now (sync, Checkpoint 1)
4 - Get task
5 - List tasks
6 - Create and submit several texts
0 - Exit
```

Example: choose `6`, type a few lines of text, press Enter on an empty line, then choose `5`.

## 9. Test and verify

```bash
gofmt -l .           # must print nothing
go vet ./...
go test ./...
go test -race ./...
go test -race -count=20 ./...   # optional: repeat to catch flaky concurrency bugs
```

On Windows, `-race` needs cgo: `set CGO_ENABLED=1` (PowerShell: `$env:CGO_ENABLED=1`)
and a C compiler (gcc, e.g. from MSYS2 / TDM-GCC) in `PATH`.

### What the concurrency tests check (`pool_test.go`)

| Requirement | Test |
|---|---|
| Pool startup | `TestWorkerPoolStartup`, `TestNewWorkerPoolValidation` |
| Asynchronous submission | `TestSubmitIsAsynchronous` |
| Two tasks really overlap | `TestTasksOverlapInProcessing` |
| Many tasks, correct per-task results and states | `TestManyTasksCorrectResultsAndStates`, `TestFailedProcessingInPool` |
| Invalid / duplicate submissions | `TestSubmitInvalidAndDuplicate`, `TestSubmitCompletedTaskRejected` |
| Full queue | `TestFullQueueRejectsAndKeepsTaskPending`, `TestRejectedTaskCanBeSubmittedLater` |
| Safe concurrent access | `TestConcurrentAccessToStore`, `TestConcurrentSubmitAndShutdown` |
| Workers terminate, accepted tasks not lost | `TestShutdownFinishesAcceptedTasks`, `TestShutdownTwiceIsSafe` |
| Rejection after shutdown | `TestSubmitAfterShutdownRejected` |

Tests do not use `time.Sleep` to wait for work. A `blockingProcessor` controls exactly when
analysis finishes: each call signals "started" on a channel and waits until `release` is closed.
If two tasks both report "started" before release, they are provably running at the same time.
`time.After` is used only as a failsafe so that a deadlock fails the test instead of hanging it.
