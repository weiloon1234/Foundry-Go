package database

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type retryFailureOwner struct {
	err   error
	calls int
}

func (o *retryFailureOwner) Transaction(context.Context, func(*Tx) error, ...TxOptions) error {
	o.calls++
	return o.err
}

type retryInspectionFailure struct {
	inspect func()
	cycle   bool
}

func (*retryInspectionFailure) Error() string   { return "private retry failure" }
func (e *retryInspectionFailure) Is(error) bool { e.inspect(); return false }
func (e *retryInspectionFailure) Unwrap() error {
	if e.cycle {
		return e
	}
	return nil
}

func TestRetryInspectsAllOutcomesWithoutEscaping(t *testing.T) {
	rolledBack := &Error{detail: Detail{Code: SerializationFailure}, outcome: RolledBack}
	for _, test := range []struct {
		name     string
		err      error
		attempts int
	}{
		{"rollback", rolledBack, 3},
		{"wrapped rollback", errors.Join(errors.New("diagnostic"), rolledBack), 3},
		{"committed sibling", errors.Join(rolledBack, &Error{detail: Detail{Code: AfterCommitFailed}, outcome: Committed}), 1},
		{"unknown sibling", errors.Join(rolledBack, &Error{detail: Detail{Code: QueryFailed}, outcome: Unknown}), 1},
		{"commit unknown code", errors.Join(rolledBack, CommitUnknown), 1},
		{"no confirmed rollback", errors.Join(rolledBack, &Error{detail: Detail{Code: Deadlock}, outcome: NoCommit}), 1},
		{"panic", errors.Join(rolledBack, &retryInspectionFailure{inspect: func() { panic("private") }}), 1},
		{"goexit", errors.Join(rolledBack, &retryInspectionFailure{inspect: runtime.Goexit}), 1},
		{"cycle", errors.Join(rolledBack, &retryInspectionFailure{inspect: func() {}, cycle: true}), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &retryFailureOwner{err: test.err}
			var result error
			returned := false
			escaped := callback.Isolated("retry caller", func() error {
				result = Retry(t.Context(), owner, RetryPolicy{Attempts: 3, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond}, func(*Tx) error { return nil })
				returned = true
				return nil
			})
			if escaped != nil || !returned || result != test.err || owner.calls != test.attempts {
				t.Fatal("retry lost its failure, escaped inspection or replayed an unsafe outcome", owner.calls)
			}
		})
	}
}
