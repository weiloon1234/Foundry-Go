package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestTransactionCommitsAndReleasesBeforeAfterCommit(t *testing.T) {
	state := &driverState{}
	state.begin = func(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
		if options.Isolation != driver.IsolationLevel(sql.LevelSerializable) || !options.ReadOnly {
			t.Error("isolation options lost")
		}
		return transaction{state}, nil
	}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen = 1; c.MaxIdle = 1 })
	var escaped *database.Tx
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		escaped = tx
		if _, err := tx.Exec(t.Context(), "insert fixture"); err != nil {
			return err
		}
		return tx.AfterCommit(func(ctx context.Context) error {
			if state.committed.Load() != 1 || tx.State() != database.TxCommitted || db.Stats().InUse != 0 || db.Stats().Owners != 1 {
				t.Error("after-commit ran before commit/release")
			}
			_, err := db.Exec(ctx, "after commit query")
			return err
		})
	}, database.TxOptions{Isolation: database.Serializable, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if state.committed.Load() != 1 || state.rolledBack.Load() != 0 || db.Stats().Owners != 0 {
		t.Fatal("incorrect transaction lifetime")
	}
	if _, err := escaped.Exec(t.Context(), "escaped query"); !errors.Is(err, database.Closed) {
		t.Fatal("escaped transaction accepted query")
	}
	if err := escaped.AfterCommit(func(context.Context) error { return nil }); !errors.Is(err, database.Closed) {
		t.Fatal("terminal callback registration accepted")
	}
}

func TestTransactionCallbackFailuresRollbackAndSuppressAfterCommit(t *testing.T) {
	cause := errors.New("private-credential")
	for name, fail := range map[string]func() error{
		"error":  func() error { return cause },
		"panic":  func() error { panic("private-credential") },
		"goexit": func() error { runtime.Goexit(); return nil },
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			db := open(t, state, nil)
			var escaped *database.Tx
			err := db.Transaction(t.Context(), func(tx *database.Tx) error {
				escaped = tx
				if err := tx.AfterCommit(func(context.Context) error { t.Error("after-commit ran on rollback"); return nil }); err != nil {
					return err
				}
				return fail()
			})
			if err == nil || state.rolledBack.Load() != 1 || state.committed.Load() != 0 || db.Stats().Owners != 0 || escaped.State() != database.TxRolledBack {
				t.Fatalf("rollback behavior: %v", err)
			}
			if name == "error" && err != cause || name != "error" && !errors.Is(err, fault.Panicked) {
				t.Fatal("failure identity lost", err)
			}
			var relabeled *database.Error
			if name == "error" {
				// An application failure is returned unchanged after a confirmed
				// rollback; its formatting belongs to the application.
				if errors.As(err, &relabeled) {
					t.Fatal("application callback failure was relabeled as a database failure")
				}
				return
			}
			// Contained panic payloads are never formatted.
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
					t.Fatal("callback failure leaked secret")
				}
			}
		})
	}
}

func TestAfterCommitFailuresPreserveCommittedOutcomeAndContinue(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	cause := errors.New("private-credential")
	var order []int
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for index, fn := range []func() error{func() error { return cause }, func() error { panic("private-credential") }, func() error { runtime.Goexit(); return nil }, func() error { return nil }} {
			if err := tx.AfterCommit(func(context.Context) error { order = append(order, index); return fn() }); err != nil {
				return err
			}
		}
		return nil
	})
	var failure *database.Error
	if !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, cause) || !errors.Is(err, fault.Panicked) || !errors.As(err, &failure) || failure.Outcome() != database.Committed {
		t.Fatalf("commit outcome: %v", err)
	}
	if !reflect.DeepEqual(order, []int{0, 1, 2, 3}) || state.committed.Load() != 1 || state.rolledBack.Load() != 0 {
		t.Fatal("callback failures skipped later work or rolled back commit")
	}
	if strings.Contains(fmt.Sprint(err), "private-credential") {
		t.Fatal("after-commit failure leaked secret")
	}
}

func TestCommitFailureIsNeverReportedAsSuccessfulRollback(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			cause := errors.New("commit response lost")
			state := &driverState{commitError: cause}
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
				return database.Detail{Code: database.SerializationFailure, CommitRejected: rejected}
			}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(t.Context())
			var escaped *database.Tx
			err = db.Transaction(t.Context(), func(tx *database.Tx) error {
				escaped = tx
				return tx.AfterCommit(func(context.Context) error { t.Error("callback on failed commit"); return nil })
			})
			var failure *database.Error
			if !errors.Is(err, cause) || !errors.As(err, &failure) || state.committed.Load() != 1 || state.rolledBack.Load() != 0 {
				t.Fatal("commit failure or attempt count lost")
			}
			if rejected {
				if failure.Outcome() != database.RolledBack || escaped.State() != database.TxRolledBack || !errors.Is(err, database.SerializationFailure) {
					t.Fatal("server rejection lost")
				}
			} else {
				if failure.Outcome() != database.Unknown || escaped.State() != database.TxUnknown || !errors.Is(err, database.CommitUnknown) {
					t.Fatal("ambiguous commit hidden")
				}
			}
		})
	}
}

func TestSavepointScopesOwnCallbacksAndParentAccess(t *testing.T) {
	state := &driverState{}
	var statements []string
	state.exec = func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
		statements = append(statements, statement)
		return driver.RowsAffected(1), nil
	}
	db := open(t, state, nil)
	cause := errors.New("recoverable child failure")
	var order []string
	var child *database.Tx
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := tx.AfterCommit(func(context.Context) error { order = append(order, "outer before"); return nil }); err != nil {
			return err
		}
		if err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			child = inner
			if _, err := tx.Exec(t.Context(), "wrong scope"); !errors.Is(err, database.Busy) {
				t.Error("parent usable during child scope")
			}
			if err := inner.AfterCommit(func(context.Context) error { order = append(order, "child"); return nil }); err != nil {
				return err
			}
			return inner.Savepoint(t.Context(), func(nested *database.Tx) error {
				return nested.AfterCommit(func(context.Context) error { order = append(order, "nested"); return nil })
			})
		}); err != nil {
			return err
		}
		if child.State() != database.TxReleased {
			t.Error("savepoint release claimed commit")
		}
		if err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			if err := inner.AfterCommit(func(context.Context) error { t.Error("rolled-back child callback ran"); return nil }); err != nil {
				return err
			}
			return cause
		}); !errors.Is(err, cause) {
			return errors.New("savepoint failure lost")
		}
		return tx.AfterCommit(func(context.Context) error { order = append(order, "outer after"); return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SAVEPOINT foundry_sp_1", "SAVEPOINT foundry_sp_2", "RELEASE SAVEPOINT foundry_sp_2", "RELEASE SAVEPOINT foundry_sp_1", "SAVEPOINT foundry_sp_3", "ROLLBACK TO SAVEPOINT foundry_sp_3", "RELEASE SAVEPOINT foundry_sp_3"}
	if !reflect.DeepEqual(statements, want) || !reflect.DeepEqual(order, []string{"outer before", "child", "nested", "outer after"}) {
		t.Fatalf("savepoint sequence: %v callbacks: %v", statements, order)
	}
	if _, err := child.Query(t.Context(), "escaped child"); !errors.Is(err, database.Closed) {
		t.Fatal("released child was reusable")
	}
}

func TestSavepointCleanupFailurePoisonsOuterCommit(t *testing.T) {
	for _, prefix := range []string{"ROLLBACK TO", "RELEASE"} {
		t.Run(prefix, func(t *testing.T) {
			cause := errors.New("savepoint cleanup failed")
			state := &driverState{exec: func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
				if strings.HasPrefix(statement, prefix) {
					return nil, cause
				}
				return driver.RowsAffected(1), nil
			}}
			db := open(t, state, nil)
			err := db.Transaction(t.Context(), func(tx *database.Tx) error {
				_ = tx.Savepoint(t.Context(), func(*database.Tx) error {
					if prefix == "ROLLBACK TO" {
						return errors.New("child failed")
					}
					return nil
				})
				return nil
			})
			if !errors.Is(err, cause) || state.committed.Load() != 0 || state.rolledBack.Load() != 1 {
				t.Fatalf("poison ignored: %v", err)
			}
		})
	}
}

func TestUnclosedTransactionRowsRejectCommitAndRelease(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	var rows *database.Rows
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		var err error
		rows, err = tx.Query(t.Context(), "select fixture")
		return err
	})
	if !errors.Is(err, database.Busy) || state.committed.Load() != 0 || state.rolledBack.Load() != 1 || db.Stats().Owners != 0 {
		t.Fatalf("unclosed rows committed/leaked: %v", err)
	}
	if rows.Next() {
		t.Fatal("transaction rows remained open")
	}
}

func TestBeginContextCancelsOperationWithIndependentContext(t *testing.T) {
	started := make(chan struct{})
	state := &driverState{exec: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	db := open(t, state, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- db.Transaction(ctx, func(tx *database.Tx) error { _, err := tx.Exec(context.Background(), "blocking query"); return err })
	}()
	<-started
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("context lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("operation escaped begin context")
	}
	if db.Stats().Owners != 0 || state.committed.Load() != 0 {
		t.Fatal("canceled transaction leaked or committed")
	}
}

func TestReturningWithConcurrentTransactionOperationAborts(t *testing.T) {
	started := make(chan struct{})
	state := &driverState{exec: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	db := open(t, state, nil)
	var work sync.WaitGroup
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		work.Go(func() { _, _ = tx.Exec(context.Background(), "blocking query") })
		<-started
		return nil
	})
	work.Wait()
	if !errors.Is(err, database.Busy) || state.committed.Load() != 0 || db.Stats().Owners != 0 {
		t.Fatalf("unfinished operation was committed: %v", err)
	}
}

func TestPoolShutdownWaitsForAfterCommitCallbackExit(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	started, finish := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	result := make(chan error, 1)
	go func() {
		result <- db.Transaction(t.Context(), func(tx *database.Tx) error {
			return tx.AfterCommit(func(context.Context) error { close(started); <-finish; return nil })
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := db.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown passed active callback: %v", err)
	}
	if db.Stats().Owners != 1 || db.Stats().InUse != 0 {
		t.Fatal("callback must retain scope, not connection")
	}
	release.Do(func() { close(finish) })
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type blockingRows struct {
	ctx     context.Context
	started chan struct{}
}

func (r *blockingRows) Columns() []string { return []string{"value"} }
func (r *blockingRows) Close() error      { return nil }
func (r *blockingRows) Next([]driver.Value) error {
	close(r.started)
	<-r.ctx.Done()
	return r.ctx.Err()
}

func TestUnfinishedRowIterationIsCanceledBeforeForcedCleanup(t *testing.T) {
	started := make(chan struct{})
	state := &driverState{query: func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
		return &blockingRows{ctx, started}, nil
	}}
	db := open(t, state, nil)
	var work sync.WaitGroup
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		rows, err := tx.Query(context.Background(), "blocking rows")
		if err != nil {
			return err
		}
		work.Go(func() { rows.Next() })
		<-started
		return nil
	})
	work.Wait()
	if !errors.Is(err, database.Busy) || db.Stats().Owners != 0 || state.committed.Load() != 0 {
		t.Fatalf("unfinished stream was leaked or committed: %v", err)
	}
}

func TestRollbackFailureRetainsCauseWithoutClaimingSuccess(t *testing.T) {
	cause := errors.New("rollback failed")
	state := &driverState{rollbackError: cause}
	db := open(t, state, nil)
	var escaped *database.Tx
	err := db.Transaction(t.Context(), func(tx *database.Tx) error { escaped = tx; return errors.New("callback failed") })
	var failure *database.Error
	if !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Outcome() != database.NoCommit || escaped.State() != database.TxUnknown {
		t.Fatalf("failed rollback reported success: %v", err)
	}
}

func TestTransactionOwnershipPropagatesThroughSessionsAndSavepoints(t *testing.T) {
	db := open(t, &driverState{}, nil)
	other := open(t, &driverState{}, nil)
	check := func(tx *database.Tx) error {
		if !tx.BelongsTo(db) || tx.BelongsTo(other) || tx.BelongsTo(nil) {
			return errors.New("incorrect transaction pool ownership")
		}
		return tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if !child.BelongsTo(db) || child.BelongsTo(other) {
				return errors.New("savepoint lost pool ownership")
			}
			return nil
		})
	}
	if err := db.Transaction(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(s *database.Session) error { return s.Transaction(t.Context(), check) }); err != nil {
		t.Fatal(err)
	}
	var absent *database.Tx
	if absent.BelongsTo(db) || (&database.Tx{}).BelongsTo(db) {
		t.Fatal("unowned transaction claimed pool")
	}
}

func TestTransactionIdentityDistinguishesPoolsTransactionsAndSavepoints(t *testing.T) {
	db := open(t, &driverState{}, nil)
	var previous *database.Tx
	for range 2 {
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			if !tx.SharesTransaction(tx) || tx.SharesTransaction(previous) || tx.SharesTransaction(nil) || tx.SharesTransaction(&database.Tx{}) {
				return errors.New("incorrect outer transaction identity")
			}
			return tx.Savepoint(t.Context(), func(child *database.Tx) error {
				if !tx.SharesTransaction(child) || !child.SharesTransaction(tx) {
					return errors.New("savepoint lost transaction identity")
				}
				return child.Savepoint(t.Context(), func(grandchild *database.Tx) error {
					if !tx.SharesTransaction(grandchild) {
						return errors.New("nested savepoint lost identity")
					}
					previous = tx
					return nil
				})
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var absent *database.Tx
	if absent.SharesTransaction(absent) || (&database.Tx{}).SharesTransaction(&database.Tx{}) {
		t.Fatal("uninitialized transaction claimed ownership")
	}
}
