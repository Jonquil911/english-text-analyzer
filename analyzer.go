package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidText = errors.New("invalid text")

type TextResult struct {
	WordCount         int
	CharacterCount    int
	UniqueWords       int
	MostFrequentWord  string
	MostFrequentCount int
}

type TextProcessor interface {
	Analyze(text string) (TextResult, error)
}

type TextAnalyzer struct{}

func ValidateText(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: text cannot be blank", ErrInvalidText)
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return nil
		}
	}
	return fmt.Errorf("%w: text must contain a letter or digit", ErrInvalidText)
}

func (TextAnalyzer) Analyze(text string) (TextResult, error) {
	if err := ValidateText(text); err != nil {
		return TextResult{}, err
	}

	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	frequency := make(map[string]int)
	mostFrequent := ""
	maxCount := 0

	for _, word := range words {
		normalized := strings.ToLower(word)
		frequency[normalized]++
		if frequency[normalized] > maxCount {
			maxCount = frequency[normalized]
			mostFrequent = normalized
		}
	}

	return TextResult{
		WordCount:         len(words),
		CharacterCount:    utf8.RuneCountInString(text),
		UniqueWords:       len(frequency),
		MostFrequentWord:  mostFrequent,
		MostFrequentCount: maxCount,
	}, nil
}
