package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func inspectConsumerEditorBatch(options []Options, inspect func() ([]Result, error)) ([]Result, error, error) {
	sources := make(map[string][]byte)
	for _, option := range options {
		if _, ok := sources[option.File]; ok {
			continue
		}
		data, err := os.ReadFile(option.File)
		if err != nil {
			return nil, nil, err
		}
		sources[option.File] = data
	}
	results, batchErr := inspect()
	var sourceErr error
	for path, original := range sources {
		current, err := os.ReadFile(path)
		if err != nil {
			sourceErr = errors.Join(sourceErr, err)
		} else if !bytes.Equal(current, original) {
			sourceErr = errors.Join(sourceErr, fmt.Errorf("language inspection changed consumer source: %s", path))
		}
	}
	return results, batchErr, sourceErr
}

func TestConsumerEditorBatchDetectsSourceMutation(t *testing.T) {
	inspectionErr := errors.New("inspection failed after returning a result")
	for _, outcome := range []struct {
		name string
		err  error
	}{{"successful", nil}, {"failed", inspectionErr}} {
		for _, mutation := range []string{"unchanged", "edited", "deleted"} {
			t.Run(outcome.name+"/"+mutation, func(t *testing.T) {
				first := filepath.Join(t.TempDir(), "first.go")
				second := filepath.Join(t.TempDir(), "second.go")
				for _, path := range []string{first, second} {
					if err := os.WriteFile(path, []byte("package fixture\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				options := []Options{{File: first}, {File: second}, {File: second}}
				results, batchErr, sourceErr := inspectConsumerEditorBatch(options, func() ([]Result, error) {
					switch mutation {
					case "edited":
						if err := os.WriteFile(second, []byte("package changed\n"), 0600); err != nil {
							t.Fatal(err)
						}
					case "deleted":
						if err := os.Remove(second); err != nil {
							t.Fatal(err)
						}
					}
					return []Result{{Operation: "hover"}}, outcome.err
				})
				if len(results) != 1 || results[0].Operation != "hover" || !errors.Is(batchErr, outcome.err) {
					t.Fatalf("lost inspection result or error: %v, %v", results, batchErr)
				}
				if mutation == "unchanged" {
					if sourceErr != nil {
						t.Fatal(sourceErr)
					}
				} else if sourceErr == nil {
					t.Fatal("inspection changed consumer source without being detected")
				}
			})
		}
	}
}
