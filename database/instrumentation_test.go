package database_test

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestQueryObserverAndSlowLogReportStatementsWithoutArguments(t *testing.T) {
	var mu sync.Mutex
	var events []database.QueryEvent
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	state := &driverState{}
	state.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
		time.Sleep(2 * time.Millisecond)
		return driver.RowsAffected(3), nil
	}
	state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &resultRows{state: state, columns: []string{"id"}, values: [][]driver.Value{{int64(1)}, {int64(2)}}}, nil
	}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(),
		database.WithQueryObserver(func(_ context.Context, event database.QueryEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		}),
		database.WithSlowQueryLog(logger, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	if _, err := db.Exec(t.Context(), "UPDATE records SET name = $1", "secret-argument"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(t.Context(), "SELECT id FROM records WHERE name = $1", "secret-argument")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), "DELETE FROM records")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[0].Operation != "exec" || events[0].Rows != 3 || events[0].Duration < 2*time.Millisecond || events[0].Role != database.PrimaryPool ||
		events[1].Operation != "query" || events[1].Rows != 2 || events[1].Code != "" || events[2].PoolWait != 0 {
		t.Fatalf("unexpected statement events: %+v", events)
	}
	if !strings.Contains(logs.String(), "slow database statement") || !strings.Contains(logs.String(), "statement_fingerprint") ||
		strings.Contains(logs.String(), "secret-argument") || strings.Contains(logs.String(), "UPDATE records") {
		t.Fatal("slow log missing or exposed statement text/arguments", logs.String())
	}
	for _, event := range events {
		if strings.Contains(event.Statement, "secret-argument") {
			t.Fatal("event exposed an argument value")
		}
	}
}

func TestQueryObserverPanicsAndFailuresAreContained(t *testing.T) {
	state := &driverState{}
	failure := errors.New("driver failure")
	state.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, failure }
	var codes []database.Code
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
		return database.Detail{Code: database.UniqueViolation, SQLState: "23505"}
	}}, database.DefaultPoolConfig(), database.WithQueryObserver(func(_ context.Context, event database.QueryEvent) {
		codes = append(codes, event.Code)
		panic("observer bug")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	if _, err := db.Exec(t.Context(), "INSERT"); !errors.Is(err, database.UniqueViolation) {
		t.Fatal("observer panic changed the statement result", err)
	}
	if len(codes) != 1 || codes[0] != database.UniqueViolation {
		t.Fatal("failure classification not observed", codes)
	}
	// Outside a Module no application logger can be bound to a bare threshold.
	if _, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), database.WithSlowQueryThreshold(time.Second)); !errors.Is(err, fault.Invalid) {
		t.Fatal("slow query threshold without a logger started", err)
	}
	for _, option := range []database.Option{database.WithQueryObserver(nil), database.WithSlowQueryLog(nil, time.Second), database.WithSlowQueryLog(slog.Default(), 0), database.WithSlowQueryThreshold(0)} {
		if _, err := database.Prepare(database.Adapter{Connector: connector{state}}, database.DefaultPoolConfig(), option); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid instrumentation accepted", err)
		}
	}
}
