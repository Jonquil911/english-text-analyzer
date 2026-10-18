# English Text Analyzer — SIS Checkpoint 1

A synchronous, in-memory application written in **Go 1.27**. Each task contains input text, a unique ID, a status, and (after processing) an analysis result.

## Features

- Create a task with English text (supports Unicode letters and numbers, too).
- Reject blank input or text containing only punctuation.
- Give each valid task a unique integer ID.
- Store multiple tasks independently in `map[int]*Task`.
- Find any task by its ID, or list all tasks in ID order.
- Analyze word counts, Unicode character counts, unique words and the most frequent word.
- Reject processing tasks that have already completed or failed.
- Report errors instead of using `panic`.

## Task lifecycle

```text
                    +------------> Completed
                    |
Pending ------> Processing
                    |
                    +------------> Failed
```

- **Pending**: created and validated, but not processed.
- **Processing**: the `Analyze` method is currently executing.
- **Completed**: the analysis succeeded; `Result` contains its output.
- **Failed**: the processor returned an error; `Error` stores its message.

Only a **Pending** task can start processing. Completed and Failed tasks cannot be processed again. Invalid input is rejected *before* a task is created, so it never receives an ID.

`Result` is a zero-value `TextResult` until processing completes. This is intentional; check `Status == StatusCompleted` before reading the result.

## Word and character rules

- A **character** means a Unicode rune, including spaces and punctuation (not UTF-8 bytes).
- A **word** means a consecutive group of Unicode letters or digits. Punctuation, hyphens and apostrophes separate words.
- Word matching ignores case (`Go`, `go`, `GO` are the same word).
- If several words have the same highest frequency, the one that **appears first** wins.

Example:

```text
Input:  "Go go GO!"
Words: 3
Characters: 9
Unique words: 1
Most frequent word: "go" (3 times)
```

## Project files

| File | Responsibility |
|------|----------------|
| `main.go` | Console menu for creating, processing, retrieving and listing tasks |
| `analyzer.go` | Validation, `TextResult`, `TextProcessor` interface and the text analyzer |
| `task.go` | `Task`, status constants, and the processing lifecycle |
| `store.go` | Task creation, map-based storage, retrieval and listing |
| `analyzer_test.go` | Table-driven tests for analysis and input validation |
| `store_test.go` | Tests for task lifecycle, errors, storage, independence and failures |
| `go.mod` | Go module and Go 1.27 language/toolchain requirement |

## Run

Install Go 1.27 (or a compatible newer version), then run from the project folder:

```bash
go run .
```

Menu options:

```text
1 - Create task
2 - Process task
3 - Get task
4 - List tasks
0 - Exit
```

Example: create a task with `Go go GO!`, select option `2` and enter the ID printed by the program.

## Test and verify

```bash
gofmt -l .
go vet ./...
go test ./...
```

`gofmt -l .` should print nothing. To format everything automatically, run `gofmt -w *.go`.

## Go design decisions (for the code defense)

- **Structs**: `Task`, `TextResult`, `TextAnalyzer`, `TaskStore` represent the domain and responsibilities.
- **Interface**: `TextProcessor` defines `Analyze(string) (TextResult, error)`. The store depends on this behavior, not on a specific implementation. Tests replace it with a fake processor to verify failure and the `Processing` state.
- **Value receiver**: `TextAnalyzer.Analyze` uses a value receiver because the analyzer has no mutable fields.
- **Pointer receiver**: `(*Task).Process` modifies the task's `Status`, `Result`, and `Error`. `(*TaskStore)` methods update the map and ID counter.
- **Maps and slices**: `map[int]*Task` provides lookup by ID; `List()` builds and sorts a slice of IDs since Go map iteration order is unspecified.
- **Value semantics**: `Get` and `List` return copies of `Task`, not the pointers stored internally, preventing callers from modifying the stored tasks.
- **Error handling**: common error categories use sentinel errors wrapped with `%w`, making `errors.Is` possible. Invalid inputs, missing tasks and illegal transitions all return errors. No normal situation calls `panic`.
- **Concurrency**: intentionally not implemented in Checkpoint 1. Later checkpoints can add synchronization to `TaskStore` and an HTTP interface without changing the text-analysis logic.

## Scope

Checkpoint 1 only: standard library, synchronous execution, memory storage, tests. No goroutines, mutexes, database, third-party dependencies, or HTTP endpoints yet.
