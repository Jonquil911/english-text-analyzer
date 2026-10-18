package main

import (
	"errors"
	"testing"
)

func TestTextAnalyzer_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  TextResult
	}{
		{
			name:  "simple English text",
			input: "Hello Go world",
			want: TextResult{
				WordCount: 3, CharacterCount: 14, UniqueWords: 3,
				MostFrequentWord: "hello", MostFrequentCount: 1,
			},
		},
		{
			name:  "case insensitive and punctuation",
			input: "Go go GO!",
			want: TextResult{
				WordCount: 3, CharacterCount: 9, UniqueWords: 1,
				MostFrequentWord: "go", MostFrequentCount: 3,
			},
		},
		{
			name:  "Unicode characters count as runes",
			input: "Привет Go",
			want: TextResult{
				WordCount: 2, CharacterCount: 9, UniqueWords: 2,
				MostFrequentWord: "привет", MostFrequentCount: 1,
			},
		},
		{
			name:  "frequency tie goes to first word",
			input: "cat cat dog dog",
			want: TextResult{
				WordCount: 4, CharacterCount: 15, UniqueWords: 2,
				MostFrequentWord: "cat", MostFrequentCount: 2,
			},
		},
		{
			name:  "numbers are words",
			input: "123 apples",
			want: TextResult{
				WordCount: 2, CharacterCount: 10, UniqueWords: 2,
				MostFrequentWord: "123", MostFrequentCount: 1,
			},
		},
	}

	analyzer := TextAnalyzer{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := analyzer.Analyze(tt.input)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Analyze(%q) = %+v; want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateText_InvalidCases(t *testing.T) {
	for _, input := range []string{"", "   \t\n ", "!?!..."} {
		t.Run(input, func(t *testing.T) {
			if err := ValidateText(input); !errors.Is(err, ErrInvalidText) {
				t.Errorf("ValidateText(%q) error = %v; want ErrInvalidText", input, err)
			}
			if _, err := (TextAnalyzer{}).Analyze(input); !errors.Is(err, ErrInvalidText) {
				t.Errorf("Analyze(%q) error = %v; want ErrInvalidText", input, err)
			}
		})
	}
}
