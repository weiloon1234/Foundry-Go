package telemetry

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestStorageInterruptionUsesCallerContext(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	deadline, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"canceled", canceled, context.Canceled, true},
		{"wrapped", canceled, safeerror.Reveal("storage", fmt.Errorf("query: %w", context.Canceled)), true},
		{"joined cancellation", canceled, errors.Join(context.Canceled, context.Canceled), true},
		{"drain deadline", deadline, context.DeadlineExceeded, true},
		{"flush timeout", t.Context(), context.DeadlineExceeded, false},
		{"live storage failure", t.Context(), database.NewError("execute", database.Unavailable), false},
		{"unavailable after cancellation", canceled, database.NewError("execute", database.Unavailable), true},
		{"live unknown commit", t.Context(), database.NewError("commit", database.CommitUnknown), false},
		{"successful completion", canceled, nil, false},
		{"independent timeout", canceled, context.DeadlineExceeded, true},
		{"storage failure", canceled, errors.New("storage failure"), true},
		{"joined failure", canceled, errors.Join(context.Canceled, errors.New("storage failure")), true},
		{"hidden joined failure", canceled, safeerror.Reveal("storage", errors.Join(context.Canceled, errors.New("storage failure"))), true},
		{"unknown commit", canceled, errors.Join(context.Canceled, database.NewError("commit", database.CommitUnknown)), true},
		{"cycle", canceled, &interruptionCycle{}, true},
		{"panic", canceled, interruptionPanic{}, true},
		{"goexit", canceled, interruptionGoexit{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := storageInterrupted(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("interrupted=%v, want %v", got, tc.want)
			}
		})
	}
}

type interruptionCycle struct{}

func (e *interruptionCycle) Error() string { return "cycle" }
func (e *interruptionCycle) Unwrap() error { return e }

type interruptionPanic struct{}

func (interruptionPanic) Error() string { return "panic" }
func (interruptionPanic) Unwrap() error { panic("private cause") }

type interruptionGoexit struct{}

func (interruptionGoexit) Error() string { return "goexit" }
func (interruptionGoexit) Unwrap() error { runtime.Goexit(); return nil }
