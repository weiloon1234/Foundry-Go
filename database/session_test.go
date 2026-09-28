package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestCanceledSessionCannotStartWorkWithAnIndependentContext(t *testing.T) {
	state := &driverState{}
	state.exec = func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
		if ctx.Err() == nil {
			t.Error("already canceled session reached driver with a live context")
		}
		return nil, ctx.Err()
	}
	db := open(t, state, nil)
	for range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		err := db.Session(ctx, func(session *database.Session) error {
			cancel()
			_, err := session.Exec(context.Background(), "must not execute")
			return err
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled session outcome: %v", err)
		}
	}
}

func TestSessionRetainsOneConnectionAcrossTransactions(t *testing.T) {
	state := &driverState{}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen = 1; c.MaxIdle = 1 })
	var escaped *database.Session
	err := db.Session(t.Context(), func(session *database.Session) error {
		escaped = session
		if _, err := session.Exec(t.Context(), "session start"); err != nil {
			return err
		}
		for range 2 {
			if err := session.Transaction(t.Context(), func(tx *database.Tx) error {
				if _, err := session.Exec(t.Context(), "outside active transaction"); !errors.Is(err, database.Busy) {
					t.Error("session bypassed transaction ownership")
				}
				var value int64
				return database.ScanOne(t.Context(), tx, "select value", nil, &value)
			}); err != nil {
				return err
			}
		}
		if state.connected.Load() != 1 || db.Stats().InUse != 1 {
			t.Error("session changed connection")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.committed.Load() != 2 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
		t.Fatal("session did not release")
	}
	if _, err := escaped.Exec(t.Context(), "escaped session"); !errors.Is(err, database.Closed) {
		t.Fatal("escaped session usable")
	}
}

func TestSessionDiscardAndFailuresPreventConnectionReuse(t *testing.T) {
	for _, mode := range []string{"explicit", "error", "panic", "goexit", "rows"} {
		t.Run(mode, func(t *testing.T) {
			state := &driverState{}
			db := open(t, state, nil)
			err := db.Session(t.Context(), func(session *database.Session) error {
				switch mode {
				case "explicit":
					session.Discard()
					return nil
				case "error":
					return errors.New("caller failed")
				case "panic":
					panic("private-credential")
				case "goexit":
					runtime.Goexit()
				case "rows":
					_, err := session.Query(t.Context(), "leftover rows")
					return err
				}
				return nil
			})
			if (mode == "explicit") != (err == nil) || db.Stats().Owners != 0 || state.closed.Load() != 1 {
				t.Fatalf("session cleanup %s: %v", mode, err)
			}
			if err := db.Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
			if state.connected.Load() != 2 {
				t.Fatal("discarded connection reused")
			}
		})
	}
}

func TestSessionErrorRetainsNestedCommitOutcome(t *testing.T) {
	state := &driverState{commitError: errors.New("commit acknowledgement lost")}
	db := open(t, state, nil)
	err := db.Session(t.Context(), func(session *database.Session) error {
		return session.Transaction(t.Context(), func(*database.Tx) error { return nil })
	})
	var detail *database.Error
	if !errors.Is(err, database.CommitUnknown) || !errors.As(err, &detail) || detail.Outcome() != database.Unknown {
		t.Fatalf("nested commit outcome lost: %v", err)
	}
	if state.closed.Load() != 1 {
		t.Fatal("uncertain commit connection retained")
	}
}

func TestClassifierPanicAndGoexitCannotStrandPoolStartup(t *testing.T) {
	for _, exit := range []bool{false, true} {
		state := &driverState{connect: func(context.Context) error { return errors.New("connect failed") }}
		db, err := database.Prepare(database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
			if exit {
				runtime.Goexit()
			}
			panic("private-credential")
		}}, database.DefaultPoolConfig())
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Start(t.Context()); !errors.Is(err, fault.Panicked) {
			t.Fatalf("classifier failure not isolated: %v", err)
		}
		if err := db.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if db.Stats().Owners != 0 {
			t.Fatal("classifier stranded startup owner")
		}
	}
}

func TestSessionTransactionCallbackFailureCanRecoverAfterConfirmedRollback(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	cause := errors.New("transaction callback failed")
	err := db.Session(t.Context(), func(session *database.Session) error {
		if err := session.Transaction(t.Context(), func(*database.Tx) error { return cause }); !errors.Is(err, cause) {
			t.Error("failure lost")
		}
		return session.Transaction(t.Context(), func(tx *database.Tx) error { _, err := tx.Exec(t.Context(), "next transaction"); return err })
	})
	if err != nil || state.committed.Load() != 1 || state.rolledBack.Load() != 1 || state.connected.Load() != 1 {
		t.Fatalf("confirmed rollback damaged session: %v", err)
	}
}
