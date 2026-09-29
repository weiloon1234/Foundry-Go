package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func runObserverOwner(t *testing.T, db *database.DB, ctx context.Context, kind string, fn func(database.Executor) error) error {
	t.Helper()
	switch kind {
	case "pool":
		return fn(db)
	case "session":
		return db.Session(ctx, func(session *database.Session) error { return fn(session) })
	case "transaction":
		return db.Transaction(ctx, func(tx *database.Tx) error { return fn(tx) })
	case "savepoint":
		return db.Transaction(ctx, func(tx *database.Tx) error {
			return tx.Savepoint(ctx, func(child *database.Tx) error { return fn(child) })
		})
	default:
		return errors.New("unknown fixture owner")
	}
}

type observerContextKey struct{}

func oneObserverConnection(config *database.PoolConfig) {
	config.MaxOpen, config.MaxIdle = 1, 1
	config.AcquireTimeout = 100 * time.Millisecond
}

func TestObserverScopeClosesRowsWithoutCancelingCallbackOrTransaction(t *testing.T) {
	for _, kind := range []string{"pool", "session", "transaction", "savepoint"} {
		t.Run(kind, func(t *testing.T) {
			state := &driverState{}
			var streamContext, retained context.Context
			state.query = func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
				streamContext = ctx
				return &resultRows{state: state, values: [][]driver.Value{{int64(42)}}}, nil
			}
			db := open(t, state, oneObserverConnection)
			queryContext := context.WithValue(t.Context(), observerContextKey{}, "query")
			callbackContext := context.WithValue(t.Context(), observerContextKey{}, "callback")
			err := runObserverOwner(t, db, t.Context(), kind, func(executor database.Executor) error {
				rows, err := (rowObserverExecutor{executor}).Query(queryContext, "SELECT 42")
				if err != nil {
					return err
				}
				defer rows.Close()
				return rows.WithObserverScope(callbackContext, func(ctx context.Context) error {
					retained = ctx
					if ctx.Value(observerContextKey{}) != "callback" {
						return errors.New("observer context values changed")
					}
					if err := rows.Close(); err != nil {
						return err
					}
					if ctx.Err() != nil || queryContext.Err() != nil {
						return errors.New("closing stream canceled the callback or query caller")
					}
					if kind != "pool" && streamContext.Err() == nil {
						return errors.New("closing scoped rows left their SQL context live")
					}
					_, err := executor.Exec(ctx, "SELECT 1")
					return err
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			if retained == nil || !errors.Is(retained.Err(), context.Canceled) {
				t.Fatal("observer context escaped its callback")
			}
			if (kind == "transaction" || kind == "savepoint") && state.committed.Load() != 1 {
				t.Fatal("observer scope prevented the completed transaction from committing")
			}
			if db.Stats().InUse != 0 || db.Stats().Owners != 0 || state.rowsClosed.Load() != 1 {
				t.Fatal("observer scope retained SQL or work ownership")
			}
		})
	}
}

func TestObserverScopeObservesQueryAndOwnerCancellationAfterRowsClose(t *testing.T) {
	for _, kind := range []string{"pool", "session", "transaction", "savepoint"} {
		for _, source := range []string{"query", "owner"} {
			if kind == "pool" && source == "owner" {
				continue
			}
			t.Run(kind+"/"+source, func(t *testing.T) {
				db := open(t, &driverState{}, oneObserverConnection)
				owner, cancelOwner := context.WithCancel(t.Context())
				defer cancelOwner()
				queryContext, cancelQuery := context.WithCancel(t.Context())
				defer cancelQuery()
				err := runObserverOwner(t, db, owner, kind, func(executor database.Executor) error {
					rows, err := executor.Query(queryContext, "SELECT 42")
					if err != nil {
						return err
					}
					defer rows.Close()
					// This context cannot erase the actual query or owner cancellation.
					return rows.WithObserverScope(context.Background(), func(ctx context.Context) error {
						if err := rows.Close(); err != nil {
							return err
						}
						if source == "query" {
							cancelQuery()
						} else {
							cancelOwner()
						}
						select {
						case <-ctx.Done():
							return nil // The scope must still report cancellation.
						case <-time.After(time.Second):
							return errors.New("observer did not observe source cancellation")
						}
					})
				})
				if !errors.Is(err, context.Canceled) {
					t.Fatal("source cancellation was lost", err)
				}
				assertCanceledObserverCleanup(t, db)
			})
		}
	}
}

func TestObserverScopePoolShutdownDrainsWorkAfterConnectionRelease(t *testing.T) {
	db := open(t, &driverState{}, oneObserverConnection)
	rows, err := db.Query(t.Context(), "SELECT 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ready, release := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	result := make(chan error, 1)
	go func() {
		result <- rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
			if err := rows.Close(); err != nil {
				return err
			}
			if _, err := db.Exec(ctx, "SELECT 1"); err != nil {
				return err
			}
			close(ready)
			<-release
			return ctx.Err()
		})
	}()
	select {
	case <-ready:
	case err := <-result:
		t.Fatal("observer exited before releasing its stream", err)
	case <-time.After(time.Second):
		t.Fatal("observer did not release its stream")
	}
	if stats := db.Stats(); stats.InUse != 0 || stats.Owners != 1 {
		t.Fatal("observer should retain work but no connection", stats)
	}
	closing, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := db.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("pool closed while callback work remained", err)
	}
	select {
	case <-db.Done():
		t.Fatal("pool reported closed before callback exit")
	default:
	}
	finish()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestObserverScopeCanRetainAnExistingStreamDuringPoolDrain(t *testing.T) {
	db := open(t, &driverState{}, oneObserverConnection)
	rows, err := db.Query(t.Context(), "SELECT 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	closing, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := db.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unclosed stream did not retain pool ownership", err)
	}
	if err := rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
		if err := rows.Close(); err != nil {
			return err
		}
		select {
		case <-db.Done():
			return errors.New("pool closed underneath its existing observer continuation")
		default:
		}
		return ctx.Err()
	}); err != nil {
		t.Fatal("existing work was mistaken for a new pool operation", err)
	}
	if err := db.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestObserverScopePrematureParentReturnCannotCommitOrReturn(t *testing.T) {
	for _, kind := range []string{"session", "transaction", "savepoint"} {
		t.Run(kind, func(t *testing.T) {
			testPrematureObserverParentReturn(t, kind)
		})
	}
}

func testPrematureObserverParentReturn(t *testing.T, kind string) {
	t.Helper()
	state := &driverState{}
	db := open(t, state, oneObserverConnection)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	observerResult, transactionResult := make(chan error, 1), make(chan error, 1)
	go func() {
		transactionResult <- runObserverOwner(t, db, t.Context(), kind, func(executor database.Executor) error {
			rows, err := executor.Query(t.Context(), "SELECT 42")
			if err != nil {
				return err
			}
			go func() {
				observerResult <- rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
					if err := rows.Close(); err != nil {
						return err
					}
					close(entered)
					<-ctx.Done()
					close(canceled)
					<-release
					if _, err := executor.Exec(context.Background(), "SELECT 1"); !errors.Is(err, database.Closed) {
						return errors.New("observer reused the expired owner")
					}
					return nil
				})
			}()
			select {
			case <-entered:
				return nil // Deliberate misuse: the observer is still running.
			case err := <-observerResult:
				return err
			case <-t.Context().Done():
				return t.Context().Err()
			}
		})
	}()
	select {
	case <-canceled:
	case err := <-transactionResult:
		t.Fatal("transaction completed before its observer was canceled", err)
	case <-time.After(time.Second):
		t.Fatal("premature parent return did not cancel its observer")
	}
	// database/sql may already discard a connection during automatic rollback.
	// The guarantee is retained framework work and no reuse, not InUse == 1.
	stats := db.Stats()
	if state.committed.Load() != 0 || stats.Owners == 0 || stats.Idle != 0 {
		t.Fatal("parent committed, released its work, or made its connection reusable", stats)
	}
	select {
	case err := <-transactionResult:
		t.Fatal("transaction returned before its callback exited", err)
	default:
	}
	finish()
	if err := <-observerResult; !errors.Is(err, context.Canceled) {
		t.Fatal("observer lost owner cancellation", err)
	}
	if err := <-transactionResult; !errors.Is(err, database.Busy) {
		t.Fatal("premature return was not reported as active-work misuse", err)
	}
	if state.committed.Load() != 0 {
		t.Fatal("failed observer scope committed")
	}
	assertCanceledObserverCleanup(t, db)
}

func TestObserverScopeFailureClosesRowsAndExpiresContext(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "goexit", "close-error"} {
		t.Run(mode, func(t *testing.T) {
			cause, closeCause := errors.New("observer veto"), errors.New("stream close failed")
			state := &driverState{}
			if mode == "close-error" {
				state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
					return &resultRows{state: state, closeError: closeCause}, nil
				}
			}
			db := open(t, state, oneObserverConnection)
			rows, err := db.Query(t.Context(), "SELECT 42")
			if err != nil {
				t.Fatal(err)
			}
			var retained context.Context
			err = rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
				retained = ctx
				switch mode {
				case "error", "close-error":
					return cause
				case "panic":
					panic("fixture panic")
				case "goexit":
					runtime.Goexit()
				}
				return nil // Intentionally leave rows open; the scope owns cleanup.
			})
			if mode == "success" && err != nil || (mode == "error" || mode == "close-error") && !errors.Is(err, cause) || (mode == "panic" || mode == "goexit") && !errors.Is(err, fault.Panicked) {
				t.Fatal("callback failure changed", err)
			}
			if mode == "close-error" && !errors.Is(err, closeCause) {
				t.Fatal("callback error hid stream cleanup failure", err)
			}
			if retained == nil || !errors.Is(retained.Err(), context.Canceled) || state.rowsClosed.Load() != 1 || db.Stats().Owners != 0 || db.Stats().InUse != 0 {
				t.Fatal("callback failure retained a context, row or resource owner")
			}
		})
	}
}

func TestObserverScopeRejectsInvalidRepeatedOrClosedUse(t *testing.T) {
	db := open(t, &driverState{}, oneObserverConnection)
	rows, err := db.Query(t.Context(), "SELECT 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	called := false
	callback := func(context.Context) error { called = true; return nil }
	if err := rows.WithObserverScope(nil, callback); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil observer context accepted", err)
	}
	if err := rows.WithObserverScope(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil observer callback accepted", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rows.WithObserverScope(canceled, callback); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled observer context accepted", err)
	}
	if err := rows.WithObserverScope(t.Context(), func(ctx context.Context) error {
		if err := rows.WithObserverScope(ctx, callback); !errors.Is(err, database.Busy) {
			return errors.New("stream accepted another observer scope")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := rows.WithObserverScope(t.Context(), callback); !errors.Is(err, database.Closed) {
		t.Fatal("closed stream revived observer work", err)
	}
	queryContext, cancelQuery := context.WithCancel(t.Context())
	other, err := db.Query(queryContext, "SELECT 42")
	if err != nil {
		cancelQuery()
		t.Fatal(err)
	}
	defer other.Close()
	cancelQuery()
	// Query cancellation also closes the rows asynchronously. If that cleanup
	// wins, the scope correctly reports Closed before inspecting its owner.
	// Either result must reject the callback and release the original work.
	if err := other.WithObserverScope(context.Background(), callback); !errors.Is(err, context.Canceled) && !errors.Is(err, database.Closed) {
		t.Fatal("replacement context accepted canceled or closed query work", err)
	}
	var empty database.Rows
	if err := empty.WithObserverScope(t.Context(), callback); !errors.Is(err, fault.Invalid) {
		t.Fatal("ownerless rows accepted observer work", err)
	}
	if called || db.Stats().Owners != 0 {
		t.Fatal("invalid observer scope invoked callback or retained work")
	}
}

// database/sql can mark a canceled connection closed before its background
// rollback finishes releasing the physical connection. Callback ownership must
// end synchronously; the standard pool counter must still drain within a bound.
func assertCanceledObserverCleanup(t *testing.T, db *database.DB) {
	t.Helper()
	if stats := db.Stats(); stats.Owners != 0 {
		t.Fatal("canceled observer retained framework work", stats)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for db.Stats().InUse != 0 {
		select {
		case <-deadline.C:
			t.Fatal("canceled connection did not finish cleanup", db.Stats())
		case <-poll.C:
		}
	}
}
