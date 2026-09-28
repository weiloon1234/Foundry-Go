package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type connector struct{ state *driverState }
type driverState struct {
	connected     atomic.Int64
	closed        atomic.Int64
	rowsClosed    atomic.Int64
	committed     atomic.Int64
	rolledBack    atomic.Int64
	begin         func(context.Context, driver.TxOptions) (driver.Tx, error)
	commitError   error
	rollbackError error
	closeHook     func() error
	connect       func(context.Context) error
	ping          func(context.Context) error
	exec          func(context.Context, string, []driver.NamedValue) (driver.Result, error)
	query         func(context.Context, string, []driver.NamedValue) (driver.Rows, error)
}

func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	if c.state.connect != nil {
		if err := c.state.connect(ctx); err != nil {
			return nil, err
		}
	}
	c.state.connected.Add(1)
	return &connection{state: c.state}, nil
}
func (c connector) Driver() driver.Driver { return c }
func (c connector) Open(string) (driver.Conn, error) {
	return nil, errors.New("test requires explicit connector")
}

type connection struct{ state *driverState }

func (c *connection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented by protocol fixture")
}
func (c *connection) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented by protocol fixture")
}
func (c *connection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if c.state.begin != nil {
		return c.state.begin(ctx, options)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return transaction{c.state}, nil
}

type transaction struct{ state *driverState }

func (tx transaction) Commit() error   { tx.state.committed.Add(1); return tx.state.commitError }
func (tx transaction) Rollback() error { tx.state.rolledBack.Add(1); return tx.state.rollbackError }
func (c *connection) Close() error {
	c.state.closed.Add(1)
	if c.state.closeHook != nil {
		return c.state.closeHook()
	}
	return nil
}
func (c *connection) Ping(ctx context.Context) error {
	if c.state.ping != nil {
		return c.state.ping(ctx)
	}
	return ctx.Err()
}
func (c *connection) ExecContext(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Result, error) {
	if c.state.exec != nil {
		return c.state.exec(ctx, statement, arguments)
	}
	return driver.RowsAffected(1), ctx.Err()
}
func (c *connection) QueryContext(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
	if c.state.query != nil {
		return c.state.query(ctx, statement, arguments)
	}
	return &resultRows{state: c.state, values: [][]driver.Value{{int64(42)}}}, ctx.Err()
}

type resultRows struct {
	columns    []string
	state      *driverState
	values     [][]driver.Value
	index      int
	nextError  error
	closeError error
}

func (r *resultRows) Columns() []string {
	if r.columns != nil {
		return r.columns
	}
	return []string{"value"}
}
func (r *resultRows) Close() error { r.state.rowsClosed.Add(1); return r.closeError }
func (r *resultRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		if r.nextError != nil {
			return r.nextError
		}
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func open(t *testing.T, state *driverState, configure func(*database.PoolConfig)) *database.DB {
	t.Helper()
	config := database.DefaultPoolConfig()
	if configure != nil {
		configure(&config)
	}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, config)
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
	return db
}

func TestPoolExecParametersAndRowOwnership(t *testing.T) {
	state := &driverState{}
	state.exec = func(_ context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
		if statement != "update records set name = $1" || len(args) != 1 || args[0].Value != "'quoted-value'" {
			t.Error("raw statement and parameters were changed")
		}
		return driver.RowsAffected(3), nil
	}
	db := open(t, state, nil)
	result, err := db.Exec(t.Context(), "update records set name = $1", "'quoted-value'")
	if err != nil || result.RowsAffected != 3 || db.Stats().Owners != 0 {
		t.Fatalf("execute: %+v %v", result, err)
	}
	rows, err := db.Query(t.Context(), "select 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if db.Stats().Owners != 1 || db.Stats().InUse != 1 {
		t.Fatal("rows did not retain connection")
	}
	columns, err := rows.Columns()
	if err != nil || !reflect.DeepEqual(columns, []string{"value"}) {
		t.Fatal("columns lost")
	}
	columns[0] = "changed"
	columns, _ = rows.Columns()
	if columns[0] != "value" {
		t.Fatal("column names were shared")
	}
	var value int64
	if !rows.Next() || rows.Scan(&value) != nil || value != 42 {
		t.Fatal("row scan failed")
	}
	if rows.Next() || rows.Err() != nil || db.Stats().Owners != 0 || state.rowsClosed.Load() != 1 {
		t.Fatal("exhaustion did not release stream")
	}
	if rows.Close() != nil || state.rowsClosed.Load() != 1 {
		t.Fatal("rows closed more than once")
	}
}

func TestPoolCloseDeadlineRetainsActiveOwnership(t *testing.T) {
	state := &driverState{}
	db := open(t, state, nil)
	rows, err := db.Query(t.Context(), "select 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := db.Close(ctx); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, database.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-db.Done():
		t.Fatal("reported closed with active owner")
	default:
	}
	if !db.Stats().Closing || db.Stats().Owners != 1 || state.closed.Load() != 0 {
		t.Fatal("active connection was closed prematurely")
	}
	if _, err := db.Exec(t.Context(), "select 1"); !errors.Is(err, database.Closed) {
		t.Fatal("closing pool accepted work")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			if err := db.Close(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()
	if state.closed.Load() != 1 {
		t.Fatal("connection was not closed exactly once")
	}
}

func TestAcquisitionDeadlineDoesNotShortenStatementContext(t *testing.T) {
	state := &driverState{}
	state.exec = func(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
		select {
		case <-time.After(20 * time.Millisecond):
			return driver.RowsAffected(1), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen = 1; c.MaxIdle = 1; c.AcquireTimeout = 5 * time.Millisecond })
	rows, err := db.Query(t.Context(), "select 42")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if _, err := db.Exec(t.Context(), "select 1"); !errors.Is(err, database.DeadlineExceeded) {
		t.Fatalf("pool exhaustion: %v", err)
	}
	if db.Stats().Owners != 1 {
		t.Fatal("failed acquisition leaked owner")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), "select 1"); err != nil {
		t.Fatalf("acquisition deadline escaped into statement: %v", err)
	}
}

func TestOpenBoundsAndConnectDeadline(t *testing.T) {
	for _, change := range []func(*database.PoolConfig){
		func(c *database.PoolConfig) { c.MaxOpen = 0 }, func(c *database.PoolConfig) { c.MaxIdle = -1 }, func(c *database.PoolConfig) { c.MaxIdle = c.MaxOpen + 1 },
		func(c *database.PoolConfig) { c.MaxLifetime = -1 }, func(c *database.PoolConfig) { c.MaxIdleTime = -1 }, func(c *database.PoolConfig) { c.ConnectTimeout = 0 }, func(c *database.PoolConfig) { c.AcquireTimeout = 0 },
	} {
		config := database.DefaultPoolConfig()
		change(&config)
		state := &driverState{}
		if _, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, config); !errors.Is(err, fault.Invalid) || state.connected.Load() != 0 {
			t.Fatal("invalid configuration connected")
		}
	}
	config := database.DefaultPoolConfig()
	config.ConnectTimeout = 5 * time.Millisecond
	state := &driverState{ping: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	if _, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}}, config); !errors.Is(err, database.DeadlineExceeded) || state.closed.Load() != 1 {
		t.Fatalf("open cleanup: %v", err)
	}
}

func TestRawErrorsRemainClassifiableWithoutFormattingSecrets(t *testing.T) {
	cause := errors.New("private-credential in SQL")
	state := &driverState{exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) { return nil, cause }}
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(error) database.Detail {
		return database.Detail{Code: database.UniqueViolation, SQLState: "23505", Constraint: "users_email_key"}
	}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	_, err = db.Exec(t.Context(), "query-with-private-credential")
	var failure *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.SQLState() != "23505" || failure.Constraint() != "users_email_key" {
		t.Fatal("classification lost")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
			t.Fatal("error leaked values")
		}
	}
}

func TestScanOneAndIterationAlwaysRelease(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				values := make([][]driver.Value, count)
				for i := range values {
					values[i] = []driver.Value{int64(i)}
				}
				return &resultRows{state: state, values: values}, nil
			}
			db := open(t, state, nil)
			var value int64
			err := database.ScanOne(t.Context(), db, "select value", nil, &value)
			if count == 0 && !errors.Is(err, database.NotFound) || count == 1 && err != nil || count == 2 && !errors.Is(err, database.TooManyRows) {
				t.Fatalf("row count %d: %v", count, err)
			}
			if db.Stats().Owners != 0 {
				t.Fatal("scan one leaked owner")
			}
		})
	}
	state := &driverState{}
	db := open(t, state, nil)
	stop := errors.New("stop")
	scan := func(row database.Row) (int64, error) { var value int64; err := row.Scan(&value); return value, err }
	err := database.ForEach(t.Context(), db, "select value", nil, scan, func(int64) error { return stop })
	if !errors.Is(err, stop) || db.Stats().Owners != 0 {
		t.Fatal("early iteration return leaked")
	}
	func() {
		defer func() {
			if recover() != "callback panic" {
				t.Error("panic changed")
			}
		}()
		_ = database.ForEach(t.Context(), db, "select value", nil, scan, func(int64) error { panic("callback panic") })
	}()
	if db.Stats().Owners != 0 {
		t.Fatal("panic leaked stream")
	}
}

func TestScanIterationAndCloseErrorsPreserveCauses(t *testing.T) {
	for _, kind := range []string{"scan", "iterate", "close"} {
		t.Run(kind, func(t *testing.T) {
			cause := errors.New("private-credential")
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				r := &resultRows{state: state, values: [][]driver.Value{{"not-an-integer"}}}
				if kind == "iterate" {
					r.nextError = cause
				}
				if kind == "close" {
					r.closeError = cause
				}
				return r, nil
			}
			db := open(t, state, nil)
			rows, err := db.Query(t.Context(), "select value")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatal("missing row")
			}
			switch kind {
			case "scan":
				var value int
				err = rows.Scan(&value)
			case "iterate":
				rows.Next()
				err = rows.Err()
			case "close":
				err = rows.Close()
			}
			if err == nil || db.Stats().Owners != 0 {
				t.Fatal("failed stream retained connection")
			}
			if kind != "scan" && !errors.Is(err, cause) {
				t.Fatal("stream cause lost")
			}
		})
	}
}
