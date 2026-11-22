package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Worker pool settings (explained in README).
const (
	workerCount   = 3
	queueCapacity = 5
)

func main() {
	store := NewTaskStore(TextAnalyzer{})
	pool, err := NewWorkerPool(store, workerCount, queueCapacity)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot start worker pool:", err)
		os.Exit(1)
	}
	// Always stop the workers and wait for accepted tasks before exiting.
	defer func() {
		fmt.Println("Shutting down: finishing accepted tasks...")
		pool.Shutdown()
		fmt.Println("All workers stopped.")
	}()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	fmt.Printf("English Text Analyzer (Checkpoint 2): %d workers, queue capacity %d\n",
		pool.Workers(), pool.QueueCapacity())

	for {
		fmt.Println("\n1 - Create task")
		fmt.Println("2 - Submit task to worker pool (async)")
		fmt.Println("3 - Process task now (sync, Checkpoint 1)")
		fmt.Println("4 - Get task")
		fmt.Println("5 - List tasks")
		fmt.Println("6 - Create and submit several texts")
		fmt.Println("0 - Exit")
		fmt.Print("Choose: ")

		if !scanner.Scan() {
			break
		}

		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			fmt.Print("Enter text: ")
			if !scanner.Scan() {
				return
			}
			task, err := store.Create(scanner.Text())
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			fmt.Printf("Created task #%d (status: %s)\n", task.ID, task.Status)

		case "2":
			id, ok := askID(scanner)
			if !ok {
				continue
			}
			// Submit returns right away; a worker processes the task later.
			if err := pool.Submit(id); err != nil {
				fmt.Println("Error:", err)
				continue
			}
			fmt.Printf("Task #%d submitted. Use option 4 to check its status.\n", id)

		case "3":
			id, ok := askID(scanner)
			if !ok {
				continue
			}
			task, err := store.Process(id)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			printTask(task)

		case "4":
			id, ok := askID(scanner)
			if !ok {
				continue
			}
			task, err := store.Get(id)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			printTask(task)

		case "5":
			tasks := store.List()
			if len(tasks) == 0 {
				fmt.Println("No tasks yet.")
			}
			for _, task := range tasks {
				fmt.Printf("#%d: %s\n", task.ID, task.Status)
			}

		case "6":
			submitMany(scanner, store, pool)

		case "0":
			return // deferred Shutdown runs here

		default:
			fmt.Println("Unknown option.")
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Input error:", err)
	}
}

// submitMany reads texts until an empty line, then creates and submits each one.
func submitMany(scanner *bufio.Scanner, store *TaskStore, pool *WorkerPool) {
	fmt.Println("Enter texts, one per line. Empty line to finish:")
	for scanner.Scan() {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			return
		}
		task, err := store.Create(text)
		if err != nil {
			fmt.Println("  Error:", err)
			continue
		}
		if err := pool.Submit(task.ID); err != nil {
			// e.g. queue full: the task stays Pending and can be submitted later
			fmt.Printf("  Task #%d created but not submitted: %v\n", task.ID, err)
			continue
		}
		fmt.Printf("  Task #%d submitted\n", task.ID)
	}
}

func askID(scanner *bufio.Scanner) (int, bool) {
	fmt.Print("Task ID: ")
	if !scanner.Scan() {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || id <= 0 {
		fmt.Println("Error: enter a positive integer ID.")
		return 0, false
	}
	return id, true
}

func printTask(task Task) {
	fmt.Printf("Task #%d | Status: %s\n", task.ID, task.Status)
	fmt.Printf("Input: %q\n", task.Input)

	if task.Status == StatusCompleted {
		fmt.Printf("Words: %d\n", task.Result.WordCount)
		fmt.Printf("Characters: %d\n", task.Result.CharacterCount)
		fmt.Printf("Unique words: %d\n", task.Result.UniqueWords)
		fmt.Printf("Most frequent word: %q (%d times)\n",
			task.Result.MostFrequentWord, task.Result.MostFrequentCount)
	} else if task.Status == StatusFailed {
		fmt.Println("Processing error:", task.Error)
	}
}
