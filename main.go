package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	store := NewTaskStore(TextAnalyzer{})
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	fmt.Println("English Text Analyzer (Checkpoint 1)")

	for {
		fmt.Println("\n1 - Create task")
		fmt.Println("2 - Process task")
		fmt.Println("3 - Get task")
		fmt.Println("4 - List tasks")
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
			task, err := store.Process(id)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			printTask(task)

		case "3":
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

		case "4":
			tasks := store.List()
			if len(tasks) == 0 {
				fmt.Println("No tasks yet.")
			}
			for _, task := range tasks {
				fmt.Printf("#%d: %s\n", task.ID, task.Status)
			}

		case "0":
			return

		default:
			fmt.Println("Unknown option.")
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Input error:", err)
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
