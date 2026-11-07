package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
)

// Hold direct caller propagation so the transaction lifetime cancels the query
// first, reproducing the ordering that used to replace a deadline with Canceled.
type deferredQueryCancellation struct {
	context.Context
	allow <-chan struct{}
}

func (deferredQueryCancellation) Value(any) any { return nil }
func (c deferredQueryCancellation) AfterFunc(fn func()) func() bool {
	return context.AfterFunc(c.Context, func() { <-c.allow; fn() })
}

func TestScopedOperationRetainsDeadlineWithLiveTransactionCaller(t *testing.T) {
	for _, operation := range []string{"query", "execute"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := errors.New("driver reported cancellation after the query deadline")
				interrupted := func(ctx context.Context) error {
					<-ctx.Done()
					return errors.Join(cause, context.Canceled)
				}
				state := &driverState{
					query: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
						return nil, interrupted(ctx)
					},
					exec: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
						return nil, interrupted(ctx)
					},
				}
				db := open(t, state, nil)
				err := db.Transaction(t.Context(), func(tx *database.Tx) error {
					caller, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					if operation == "execute" {
						_, err := tx.Exec(caller, "blocked write")
						return err
					}
					_, err := tx.Query(caller, "blocked read")
					return err
				})
				var failure *database.Error
				if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Code() != database.DeadlineExceeded || failure.Outcome() != database.RolledBack {
					t.Fatalf("scoped operation lost caller deadline: %v", err)
				}
				if state.committed.Load() != 0 || state.rolledBack.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
					t.Fatal("deadline changed rollback or resource ownership")
				}
			})
		})
	}
}

func TestTransactionDeadlineCorrectionPreservesOtherFailures(t *testing.T) {
	cycle := &cyclicTransactionError{}
	for _, test := range []struct {
		name  string
		cause error
		code  database.Code
	}{
		{"native cancellation", context.Canceled, database.DeadlineExceeded},
		{"cancellation code", database.NewError("canceled callback", database.Canceled), database.DeadlineExceeded},
		{"server cancellation", errors.Join(database.NewError("statement timeout", database.QueryCanceled), context.Canceled), database.QueryCanceled},
		{"constraint failure", database.NewError("constraint", database.UniqueViolation), database.UniqueViolation},
		{"incomplete graph", errors.Join(context.Canceled, cycle), database.QueryFailed},
		{"inspection panic", &databaseClassificationError{method: "Is", inspect: func() { panic("private deadline cause") }}, database.QueryFailed},
		{"inspection goexit", &databaseClassificationError{method: "Is", inspect: runtime.Goexit}, database.QueryFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := &driverState{}
				db := open(t, state, nil)
				caller, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				err := db.Transaction(caller, func(*database.Tx) error {
					<-caller.Done()
					return test.cause
				})
				var failure *database.Error
				if !errors.As(err, &failure) || failure.Code() != test.code || !errors.Is(err, test.cause) {
					t.Fatal("deadline correction changed failure precedence or cause identity")
				}
				if test.code == database.DeadlineExceeded && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("corrected code lost the caller's deadline identity")
				}
				if failure.Outcome() != database.NoCommit && failure.Outcome() != database.RolledBack {
					t.Fatal("deadline correction changed transaction outcome", failure.Outcome())
				}
				if state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 || cycle.visits > 256 {
					t.Fatal("deadline inspection exceeded its bounds or retained ownership")
				}
			})
		})
	}
}

func TestTransactionQueryRetainsCallerDeadlineThroughScopeCancellation(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel"} {
		for _, operation := range []string{"caller", "independent"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					caller, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					allow := make(chan struct{})
					defer close(allow)
					var queryContext context.Context = deferredQueryCancellation{Context: caller, allow: allow}
					if operation == "independent" {
						queryContext = context.Background()
					}
					cause := errors.New("driver query interrupted")
					queried := false
					var driverErr error
					state := &driverState{query: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
						queried = true
						if mode == "cancel" {
							cancel()
						}
						// Virtual time advances only once the query is blocked.
						<-ctx.Done()
						driverErr = ctx.Err()
						return nil, errors.Join(cause, driverErr)
					}}
					db := open(t, state, nil)
					err := db.Transaction(caller, func(tx *database.Tx) error {
						rows, err := tx.Query(queryContext, "select locked owner")
						if rows != nil {
							_ = rows.Close()
						}
						return err
					})
					want, code := context.DeadlineExceeded, database.DeadlineExceeded
					if mode == "cancel" {
						want, code = context.Canceled, database.Canceled
					}
					var failure *database.Error
					if !queried || driverErr != context.Canceled || !errors.Is(err, want) || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Code() != code {
						t.Fatalf("transaction lost query cause or classification: %v", err)
					}
					if mode == "cancel" && errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("caller cancellation was relabeled as a deadline")
					}
					if failure.Outcome() != database.NoCommit && failure.Outcome() != database.RolledBack {
						t.Fatal("canceled callback reported a commit outcome", failure.Outcome())
					}
					if state.committed.Load() != 0 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
						t.Fatal("canceled transaction committed or retained its connection")
					}
				})
			})
		}
	}
}
