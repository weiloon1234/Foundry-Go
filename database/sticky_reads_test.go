package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestStickyReadsRouteToPrimaryAfterAWriteInScope(t *testing.T) {
	primary, read := &driverState{}, &driverState{}
	var primaryReads, replicaReads atomic.Int64
	counting := func(state *driverState, count *atomic.Int64) func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			count.Add(1)
			return &resultRows{state: state, values: [][]driver.Value{{int64(1)}}}, nil
		}
	}
	primary.query, read.query = counting(primary, &primaryReads), counting(read, &replicaReads)
	primary.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		return driver.RowsAffected(1), nil
	}
	config := database.DefaultPoolConfig()
	config.MaxOpen, config.MaxIdle = 2, 1
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{primary}}, config, readOption(read, config, 4), database.WithStickyReads(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	readOnce := func(ctx context.Context) {
		t.Helper()
		rows, err := db.QueryRead(ctx, "SELECT 1")
		if err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	scope := database.StickyReads(t.Context())
	readOnce(scope)
	if replicaReads.Load() != 1 {
		t.Fatal("a read before any write left the replica")
	}
	if _, err := db.Exec(scope, "UPDATE records SET name = $1", "x"); err != nil {
		t.Fatal(err)
	}
	readOnce(scope)
	readOnce(t.Context()) // another request is unaffected
	if primaryReads.Load() != 1 || replicaReads.Load() != 2 {
		t.Fatal("read-your-writes routing", primaryReads.Load(), replicaReads.Load())
	}
	time.Sleep(60 * time.Millisecond)
	readOnce(scope)
	if replicaReads.Load() != 3 {
		t.Fatal("stickiness outlived its window")
	}
	if err := db.Transaction(scope, func(*database.Tx) error { return nil }, database.TxOptions{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	readOnce(scope)
	if replicaReads.Load() != 4 {
		t.Fatal("a read-only transaction counted as a write")
	}
	if err := db.Transaction(scope, func(*database.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	readOnce(scope)
	if primaryReads.Load() != 2 {
		t.Fatal("a write transaction did not make reads sticky")
	}
	var handled context.Context
	database.StickyReadsHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { handled = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if handled == nil || database.StickyReads(handled) != handled {
		t.Fatal("handler did not install one request scope")
	}
	if _, err := database.Prepare(database.Adapter{Connector: connector{primary}}, config, database.WithStickyReads(0)); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero sticky window accepted")
	}
}

// The window starts again when a write completes, so a write longer than the
// window does not consume it; session statements and primary queries count.
func TestStickyReadsMeasureFromWriteCompletion(t *testing.T) {
	primary, read := &driverState{}, &driverState{}
	var primaryReads, replicaReads atomic.Int64
	counting := func(state *driverState, count *atomic.Int64) func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return func(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
			if statement == "SELECT 1" {
				count.Add(1)
			}
			return &resultRows{state: state, values: [][]driver.Value{{int64(1)}}}, nil
		}
	}
	primary.query, read.query = counting(primary, &primaryReads), counting(read, &replicaReads)
	primary.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		time.Sleep(80 * time.Millisecond)
		return driver.RowsAffected(1), nil
	}
	config := database.DefaultPoolConfig()
	config.MaxOpen, config.MaxIdle = 2, 1
	window := 50 * time.Millisecond
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{primary}}, config, readOption(read, config, 4), database.WithStickyReads(window))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	readOnce := func(ctx context.Context) {
		t.Helper()
		rows, err := db.QueryRead(ctx, "SELECT 1")
		if err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	expectPrimary := func(label string, write func(context.Context)) {
		t.Helper()
		time.Sleep(2 * window) // let any earlier window expire
		scope := database.StickyReads(t.Context())
		before := primaryReads.Load()
		write(scope)
		readOnce(scope)
		if primaryReads.Load() != before+1 {
			t.Fatal(label, "did not keep reads on the primary after completing")
		}
	}
	expectPrimary("long Exec", func(ctx context.Context) {
		if _, err := db.Exec(ctx, "UPDATE records SET name = 'x'"); err != nil {
			t.Fatal(err)
		}
	})
	expectPrimary("long transaction", func(ctx context.Context) {
		if err := db.Transaction(ctx, func(*database.Tx) error { time.Sleep(80 * time.Millisecond); return nil }); err != nil {
			t.Fatal(err)
		}
	})
	expectPrimary("session Exec", func(ctx context.Context) {
		if err := db.Session(ctx, func(session *database.Session) error {
			_, err := session.Exec(ctx, "UPDATE records SET name = 'y'")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
	expectPrimary("primary Query", func(ctx context.Context) {
		rows, err := db.Query(ctx, "INSERT INTO records DEFAULT VALUES RETURNING id")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Millisecond)
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if replicaReads.Load() != 0 {
		t.Fatal("a read left the primary inside its window", replicaReads.Load())
	}
}
