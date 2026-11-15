package capturedisclosure

import (
	"slices"
	"strings"
	"testing"
)

func TestValidateNames(t *testing.T) {
	for _, test := range []struct {
		name  string
		names []string
		want  error
	}{
		{"absent", nil, nil},
		{"empty list", []string{}, nil},
		{"count boundary", slices.Repeat([]string{"query"}, 64), nil},
		{"over count", slices.Repeat([]string{"query"}, 65), ErrTooManyNames},
		{"count precedes name validation", make([]string, 65), ErrTooManyNames},
		{"empty name", []string{""}, ErrInvalidName},
		{"length boundary", []string{strings.Repeat("x", 128)}, nil},
		{"over length", []string{strings.Repeat("x", 129)}, ErrInvalidName},
		{"unicode length boundary", []string{strings.Repeat("é", 64)}, nil},
		{"unicode over byte length", []string{strings.Repeat("é", 65)}, ErrInvalidName},
		{"nul", []string{"query\x00"}, ErrInvalidName},
		{"newline", []string{"query\n"}, ErrInvalidName},
		{"tab", []string{"query\t"}, ErrInvalidName},
		{"delete", []string{"query\x7f"}, ErrInvalidName},
		{"unicode control", []string{"query\u0085"}, ErrInvalidName},
		{"spaces and duplicates", []string{" query ", "Query", "Query"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateNames(test.names); got != test.want {
				t.Fatalf("ValidateNames returned %v, want %v", got, test.want)
			}
		})
	}
}
