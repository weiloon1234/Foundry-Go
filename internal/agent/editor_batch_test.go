package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func retryEditorChunk(t *testing.T, ctx context.Context, inspect func() ([]Result, error)) ([]Result, error) {
	t.Helper()
	results, err := inspect()
	var failure *batchFailure
	if !errors.As(err, &failure) || failure.responded || ctx.Err() != nil ||
		errors.Is(failure.cause, context.Canceled) || errors.Is(failure.cause, context.DeadlineExceeded) ||
		!closedEditorPipe(failure.cause) {
		return results, err
	}
	t.Logf("gopls chunk died before its first inspection response; retrying once on a fresh server: %v", err)
	results, retryErr := inspect()
	if retryErr != nil {
		return results, fmt.Errorf("gopls chunk failed after one early-death retry; first attempt: %w; fresh server: %w", err, retryErr)
	}
	return results, nil
}

func closedEditorPipe(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EPIPE)
}

func TestEditorChunkRetriesOnlyEarlyServerDeath(t *testing.T) {
	for _, tc := range []struct {
		mode                string
		attempts, completed int
	}{
		{"batch-die-initialize", 2, 3},
		{"batch-die-before-response", 2, 3},
		{"batch-close-pipe-before-response", 2, 3},
		{"batch-die-after-response", 1, 1},
		{"batch-close-pipe-after-response", 1, 0},
		{"batch-error", 1, 1},
		{"batch-hang", 1, 1},
		{"hang", 1, 0},
		{"malformed", 1, 0},
		{"badid", 1, 0},
		{"rpcerror", 1, 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			options, _ := batchFixture(t)
			attempts := 0
			results, err := retryEditorChunk(t, t.Context(), func() ([]Result, error) {
				mode := tc.mode
				if attempts != 0 {
					mode = "batch"
				}
				attempts++
				return inspectBatch(t.Context(), options, 500*time.Millisecond, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", mode}, 300*time.Millisecond)
			})
			if attempts != tc.attempts || len(results) != tc.completed || (err == nil) != (tc.attempts == 2) {
				t.Fatalf("chunk retry: %d attempts, %d results, %v", attempts, len(results), err)
			}
		})
	}
}

func TestEditorChunkReportsBothDeaths(t *testing.T) {
	options, _ := batchFixture(t)
	var failures []error
	_, err := retryEditorChunk(t, t.Context(), func() ([]Result, error) {
		results, err := inspectBatch(t.Context(), options, time.Second, os.Args[0], []string{"-test.run=^TestLanguageServerHelper$", "--", "batch-die-before-response"}, shutdownGrace)
		failures = append(failures, err)
		return results, err
	})
	if len(failures) != 2 || err == nil || !strings.Contains(err.Error(), "first attempt:") || !strings.Contains(err.Error(), "fresh server:") {
		t.Fatalf("lost bounded retry or both failures: %d attempts, %v", len(failures), err)
	}
	for _, failure := range failures {
		if !errors.Is(err, failure) {
			t.Fatal("lost attempt error identity", err)
		}
	}
}

func TestEditorChunkDoesNotRetryAssertionsOrCleanupErrors(t *testing.T) {
	for _, failure := range []error{
		nil,
		io.EOF, // Cleanup alone is not evidence of an early operation failure.
		errors.Join(&batchFailure{cause: errors.New("protocol error")}, io.EOF),
		&batchFailure{cause: errors.Join(io.EOF, context.DeadlineExceeded)},
		&batchFailure{cause: errors.Join(io.EOF, context.Canceled)},
	} {
		attempts := 0
		results, err := retryEditorChunk(t, t.Context(), func() ([]Result, error) {
			attempts++
			return []Result{{Payload: []byte(`{"items":[]}`)}}, failure
		})
		if attempts != 1 || len(results) != 1 || err != failure || string(results[0].Payload) != `{"items":[]}` {
			t.Fatal("retried or changed assertion payload/cleanup failure", attempts, results, err)
		}
	}
}

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
