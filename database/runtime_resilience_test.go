package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func waitFor(t *testing.T, condition func() bool, failure string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(failure)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestForgottenRowsReleaseConnectionWhenQueryContextEnds(t *testing.T) {
	state := &driverState{}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen, c.MaxIdle = 1, 1 })
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := db.Query(ctx, "forgotten"); err != nil {
		t.Fatal(err)
	}
	if db.Stats().Owners != 1 || db.Stats().InUse != 1 {
		t.Fatal("stream did not own its connection")
	}
	cancel()
	waitFor(t, func() bool { return db.Stats().Owners == 0 && db.Stats().InUse == 0 }, "canceled stream retained its pool connection")
	if _, err := db.Exec(t.Context(), "reuse"); err != nil {
		t.Fatal("released connection is not reusable", err)
	}
}

func TestForgottenTransactionRowsReleaseScopeWhenQueryContextEnds(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if _, err := tx.Query(ctx, "forgotten"); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), "overlap"); !errors.Is(err, database.Busy) {
			t.Error("open stream did not own the transaction scope")
		}
		cancel()
		var err error
		waitFor(t, func() bool { _, err = tx.Exec(t.Context(), "after cancel"); return !errors.Is(err, database.Busy) }, "canceled stream retained the transaction scope")
		return err
	})
	if err != nil || state.committed.Load() != 1 || db.Stats().Owners != 0 {
		t.Fatal("transaction did not continue after its canceled stream closed", err)
	}
}

// contextTransaction reports whether its BEGIN context was canceled while the
// driver executed COMMIT, as pgx does for its stored transaction context.
type contextTransaction struct {
	ctx      context.Context
	onCommit func()
	block    bool
}

func (tx contextTransaction) Commit() error {
	if tx.onCommit != nil {
		tx.onCommit()
	}
	if tx.block {
		<-tx.ctx.Done()
	}
	return tx.ctx.Err()
}
func (tx contextTransaction) Rollback() error { return nil }

func TestCommitIsDetachedFromCallerCancellation(t *testing.T) {
	caller, disconnect := context.WithCancel(t.Context())
	defer disconnect()
	state := &driverState{}
	state.begin = func(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
		return contextTransaction{ctx: ctx, onCommit: disconnect}, nil
	}
	db := open(t, state, nil)
	var escaped *database.Tx
	if err := db.Transaction(caller, func(tx *database.Tx) error { escaped = tx; return nil }); err != nil {
		t.Fatal("client disconnect during COMMIT changed a confirmed commit", err)
	}
	if escaped.State() != database.TxCommitted || db.Stats().Owners != 0 {
		t.Fatal("commit state or ownership was lost")
	}
}

func TestCommitTimeoutBoundsDetachedCompletion(t *testing.T) {
	state := &driverState{}
	state.begin = func(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
		return contextTransaction{ctx: ctx, block: true}, nil
	}
	db := open(t, state, func(c *database.PoolConfig) { c.CommitTimeout = 20 * time.Millisecond })
	started := time.Now()
	err := db.Transaction(t.Context(), func(*database.Tx) error { return nil })
	var detail *database.Error
	if !errors.Is(err, database.CommitUnknown) || !errors.As(err, &detail) || detail.Outcome() != database.Unknown || time.Since(started) > time.Second {
		t.Fatal("stalled COMMIT was not bounded as an unknown outcome", err)
	}
}

func TestStartRetriesTransientConnectionFailuresWithinBudget(t *testing.T) {
	transient := errors.New("database starting")
	var attempts atomic.Int32
	state := &driverState{connect: func(context.Context) error {
		if attempts.Add(1) < 3 {
			return transient
		}
		return nil
	}}
	classify := func(err error) database.Detail {
		if errors.Is(err, transient) {
			return database.Detail{Code: database.Unavailable, SQLState: "57P03"}
		}
		return database.Detail{Code: database.QueryFailed}
	}
	config := database.DefaultPoolConfig()
	config.StartupTimeout = 5 * time.Second
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: classify}, config)
	if err != nil {
		t.Fatal("transient startup failure was not retried", err)
	}
	defer db.Close(context.Background())
	if attempts.Load() != 3 {
		t.Fatal("unexpected startup attempts", attempts.Load())
	}
}

func TestStartDoesNotRetryAuthenticationOrDisabledBudget(t *testing.T) {
	for name, detail := range map[string]database.Detail{
		"authentication": {Code: database.Unavailable, SQLState: "28P01"},
		"disabled":       {Code: database.Unavailable, SQLState: "57P03"},
	} {
		t.Run(name, func(t *testing.T) {
			var attempts atomic.Int32
			state := &driverState{connect: func(context.Context) error { attempts.Add(1); return errors.New("refused") }}
			config := database.DefaultPoolConfig()
			if name == "disabled" {
				config.StartupTimeout = 0
			}
			_, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail { return detail }}, config)
			if !errors.Is(err, database.Unavailable) || attempts.Load() != 1 {
				t.Fatal("non-retryable startup failure was retried", attempts.Load(), err)
			}
		})
	}
}

func TestReadinessProbeDoesNotQueueBehindSaturatedPool(t *testing.T) {
	state := &driverState{}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen, c.MaxIdle = 1, 1 })
	holding, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.Transaction(t.Context(), func(*database.Tx) error { close(holding); <-release; return nil })
	}()
	<-holding
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, probe := range db.ReadinessProbes("database", "database-read") {
		if err := probe.Check(ctx); err != nil {
			t.Fatal("readiness queued behind application work", err)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if db.Stats().Owners != 0 {
		t.Fatal("probe retained a pool owner")
	}
}

func TestAutocommitStatementFailuresCarryOutcome(t *testing.T) {
	rejected, lost := errors.New("rejected"), errors.New("lost")
	classify := func(err error) database.Detail {
		return database.Detail{Code: database.CheckViolation, StatementRejected: errors.Is(err, rejected)}
	}
	for name, cause := range map[string]error{"rejected": rejected, "lost": lost, "not-performed": driver.ErrBadConn} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return nil, cause }}
			db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: classify}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			_, err = db.Primary().FoundryAutocommitQuery(sqlowner.Seal{}, t.Context(), "INSERT RETURNING")
			var detail *database.Error
			if !errors.As(err, &detail) || db.Stats().Owners != 0 {
				t.Fatal("autocommit failure was not classified", err)
			}
			switch name {
			case "rejected":
				if detail.Outcome() != database.RolledBack || detail.Code() != database.CheckViolation {
					t.Fatal("confirmed rejection lost its outcome", err)
				}
			case "lost":
				if detail.Outcome() != database.Unknown || detail.Code() != database.CommitUnknown {
					t.Fatal("uncertain statement was not reported as unknown", err)
				}
			default:
				if detail.Outcome() != database.NoCommit {
					t.Fatal("unperformed statement was not reported as uncommitted", err)
				}
			}
		})
	}
	state := &driverState{}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, values: [][]driver.Value{{int64(1)}}, nextError: lost}, nil
	}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: classify}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	rows, err := db.FoundryAutocommitQuery(sqlowner.Seal{}, t.Context(), "INSERT RETURNING")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	var detail *database.Error
	if err := rows.Err(); !errors.As(err, &detail) || detail.Outcome() != database.Unknown || rows.Close() == nil || db.Stats().Owners != 0 {
		t.Fatal("stream failure after rows did not report an unknown outcome", err)
	}
}
