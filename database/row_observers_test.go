package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

// Forward only execution capabilities, without exposing an observer accessor.
type rowObserverExecutor struct{ inner database.Executor }

func (w rowObserverExecutor) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	return w.inner.Exec(ctx, sql, args...)
}
func (w rowObserverExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	return w.inner.Query(ctx, sql, args...)
}

type otherRowObserverModel struct{}

func rowObserverPool(t *testing.T, state *driverState, set lifecycle.Observers) *database.DB {
	t.Helper()
	db, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := db.BindObservers(set); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func checkRowObserverOwner(rows *database.Rows, first bool) error {
	set := rows.Observers()
	if lifecycle.HasObservers[observedRecord](set) != first || lifecycle.HasObservers[otherRowObserverModel](set) == first {
		return errors.New("row stream lost its actual database's observer ownership")
	}
	return nil
}

func TestRowsRetainObserversAcrossWrappersAndClosedScopes(t *testing.T) {
	var calls atomic.Int64
	factory := func() registeredHooks { calls.Add(1); return registeredHooks{} }
	first, err := lifecycle.NewObserver[observedRecord, registeredHooks]("first").Declare(factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lifecycle.NewObserver[otherRowObserverModel, registeredHooks]("second").Declare(factory)
	if err != nil {
		t.Fatal(err)
	}
	firstSet, err := lifecycle.NewObservers(first)
	if err != nil {
		t.Fatal(err)
	}
	secondSet, err := lifecycle.NewObservers(second)
	if err != nil {
		t.Fatal(err)
	}
	db := rowObserverPool(t, &driverState{}, firstSet)
	other := rowObserverPool(t, &driverState{}, secondSet)
	var retained []*database.Rows
	read := func(executor database.Executor, first bool) error {
		rows, err := (rowObserverExecutor{executor}).Query(t.Context(), "SELECT 42")
		if err != nil {
			return err
		}
		defer rows.Close()
		if err := checkRowObserverOwner(rows, first); err != nil {
			return err
		}
		if !rows.Next() {
			return errors.New("expected one fixture row")
		}
		var value int64
		if err := rows.Scan(&value); err != nil {
			return err
		}
		if rows.Next() || rows.Err() != nil || value != 42 {
			return errors.New("row iteration changed while preserving observer metadata")
		}
		if first {
			retained = append(retained, rows)
		}
		return checkRowObserverOwner(rows, first)
	}
	if err := read(db, true); err != nil {
		t.Fatal(err)
	}
	transaction := func(tx *database.Tx) error {
		if err := read(tx, true); err != nil {
			return err
		}
		return tx.Savepoint(t.Context(), func(child *database.Tx) error { return read(child, true) })
	}
	if err := db.Transaction(t.Context(), transaction); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(session *database.Session) error {
		if err := read(session, true); err != nil {
			return err
		}
		return session.Transaction(t.Context(), transaction)
	}); err != nil {
		t.Fatal(err)
	}
	if err := read(other, false); err != nil {
		t.Fatal(err)
	}
	closeContext, cancelClose := context.WithTimeout(t.Context(), time.Second)
	defer cancelClose()
	if err := db.Close(closeContext); err != nil {
		t.Fatal("retained closed rows prevented pool cleanup", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, rows := range retained {
				if err := checkRowObserverOwner(rows, true); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if calls.Load() != 0 {
		t.Fatal("row ownership inspection instantiated write hooks")
	}
}

func TestRowsRetainObserversAfterFailedOrEarlyClose(t *testing.T) {
	declaration, err := lifecycle.NewObserver[observedRecord, registeredHooks]("retained").Declare(func() registeredHooks {
		t.Error("reading metadata invoked a hook factory")
		return registeredHooks{}
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(declaration)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"early", "scan", "iterate", "close"} {
		t.Run(mode, func(t *testing.T) {
			state := &driverState{}
			cause := errors.New("fixture stream failure")
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				rows := &resultRows{state: state, values: [][]driver.Value{{int64(42)}}}
				if mode == "iterate" {
					rows.nextError = cause
				}
				if mode == "close" {
					rows.closeError = cause
				}
				return rows, nil
			}
			db := rowObserverPool(t, state, set)
			rows, err := (rowObserverExecutor{db}).Query(t.Context(), "SELECT 42")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			switch mode {
			case "early", "close":
				err = rows.Close()
			case "scan":
				if !rows.Next() {
					t.Fatal("expected one fixture row")
				}
				err = rows.Scan() // Incorrect arity closes the stream.
			case "iterate":
				for rows.Next() {
					var value int64
					if err := rows.Scan(&value); err != nil {
						t.Fatal(err)
					}
				}
				err = rows.Err()
			}
			if (err != nil) != (mode != "early") || (mode == "iterate" || mode == "close") && !errors.Is(err, cause) {
				t.Fatal("stream failure behavior changed", err)
			}
			if err := checkRowObserverOwner(rows, true); err != nil {
				t.Fatal(err)
			}
			if state.rowsClosed.Load() != 1 {
				t.Fatal("stream did not close exactly once")
			}
			if _, err := db.Exec(t.Context(), "SELECT 1"); err != nil {
				t.Fatal("stream retained execution ownership", err)
			}
		})
	}
}
