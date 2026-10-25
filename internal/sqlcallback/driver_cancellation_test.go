package sqlcallback

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync/atomic"
	"testing"
)

type legacyState struct {
	cancelPrepare, cancelBegin context.CancelFunc
	prepares, begins           atomic.Int64
	statementCloses, rollbacks atomic.Int64
	execs, queries             atomic.Int64
}

type legacyConnector struct{ state *legacyState }

func (c legacyConnector) Connect(context.Context) (driver.Conn, error) {
	return &legacyConnection{state: c.state}, nil
}
func (c legacyConnector) Driver() driver.Driver { return c }
func (c legacyConnector) Open(string) (driver.Conn, error) {
	return c.Connect(context.Background())
}

type legacyConnection struct{ state *legacyState }

func (c *legacyConnection) Prepare(string) (driver.Stmt, error) {
	c.state.prepares.Add(1)
	if c.state.cancelPrepare != nil {
		c.state.cancelPrepare()
	}
	return &legacyStatement{state: c.state}, nil
}
func (*legacyConnection) Close() error { return nil }
func (c *legacyConnection) Begin() (driver.Tx, error) {
	c.state.begins.Add(1)
	if c.state.cancelBegin != nil {
		c.state.cancelBegin()
	}
	return legacyTransaction{state: c.state}, nil
}
func (c *legacyConnection) Exec(string, []driver.Value) (driver.Result, error) {
	c.state.execs.Add(1)
	return driver.RowsAffected(1), nil
}
func (c *legacyConnection) Query(string, []driver.Value) (driver.Rows, error) {
	c.state.queries.Add(1)
	return legacyRows{}, nil
}

type legacyStatement struct{ state *legacyState }

func (s *legacyStatement) Close() error { s.state.statementCloses.Add(1); return nil }
func (*legacyStatement) NumInput() int  { return 1 }
func (s *legacyStatement) Exec([]driver.Value) (driver.Result, error) {
	s.state.execs.Add(1)
	return driver.RowsAffected(1), nil
}
func (s *legacyStatement) Query([]driver.Value) (driver.Rows, error) {
	s.state.queries.Add(1)
	return legacyRows{}, nil
}

type legacyTransaction struct{ state *legacyState }

func (legacyTransaction) Commit() error     { return nil }
func (t legacyTransaction) Rollback() error { t.state.rollbacks.Add(1); return nil }

type legacyRows struct{}

func (legacyRows) Columns() []string         { return []string{"value"} }
func (legacyRows) Close() error              { return nil }
func (legacyRows) Next([]driver.Value) error { return io.EOF }

type cancelingValuer struct {
	cancel context.CancelFunc
	calls  *atomic.Int64
}

func (v cancelingValuer) Value() (driver.Value, error) {
	v.calls.Add(1)
	v.cancel()
	return int64(1), nil
}

func TestLegacyExecutionRejectsCancellationDuringConversion(t *testing.T) {
	for _, operation := range []string{"connection exec", "connection query", "statement exec", "statement query"} {
		t.Run(operation, func(t *testing.T) {
			state := &legacyState{}
			db := sql.OpenDB(Connector(legacyConnector{state}))
			defer db.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var conversions atomic.Int64
			argument := cancelingValuer{cancel, &conversions}
			var err error
			switch operation {
			case "connection exec":
				_, err = db.ExecContext(ctx, "execute", argument)
			case "connection query":
				var rows *sql.Rows
				rows, err = db.QueryContext(ctx, "query", argument)
				if rows != nil {
					rows.Close()
				}
			default:
				statement, prepareErr := db.PrepareContext(ctx, "prepared")
				if prepareErr != nil {
					t.Fatal(prepareErr)
				}
				defer statement.Close()
				if operation == "statement exec" {
					_, err = statement.ExecContext(ctx, argument)
				} else {
					var rows *sql.Rows
					rows, err = statement.QueryContext(ctx, argument)
					if rows != nil {
						rows.Close()
					}
				}
			}
			if !errors.Is(err, context.Canceled) || conversions.Load() != 1 {
				t.Fatal("cancellation during conversion was not returned")
			}
			if state.execs.Load() != 0 || state.queries.Load() != 0 || db.Stats().InUse != 0 {
				t.Fatal("canceled conversion executed a legacy operation or retained a connection")
			}
		})
	}
}

func TestLegacyCanceledPrepareAndBeginCleanUpOnce(t *testing.T) {
	for _, operation := range []string{"prepare", "begin"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := &legacyState{}
			if operation == "prepare" {
				state.cancelPrepare = cancel
			} else {
				state.cancelBegin = cancel
			}
			db := sql.OpenDB(Connector(legacyConnector{state}))
			defer db.Close()
			var err error
			if operation == "prepare" {
				statement, failure := db.PrepareContext(ctx, "prepared")
				err = failure
				if statement != nil {
					statement.Close()
					t.Fatal("canceled prepare published a statement")
				}
				if state.prepares.Load() != 1 || state.statementCloses.Load() != 1 {
					t.Fatal("canceled prepare did not close exactly once")
				}
			} else {
				transaction, failure := db.BeginTx(ctx, nil)
				err = failure
				if transaction != nil {
					transaction.Rollback()
					t.Fatal("canceled begin published a transaction")
				}
				if state.begins.Load() != 1 || state.rollbacks.Load() != 1 {
					t.Fatal("canceled begin did not roll back exactly once")
				}
			}
			if !errors.Is(err, context.Canceled) || db.Stats().InUse != 0 {
				t.Fatal("canceled acquisition lost cancellation or retained its connection")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if operation == "prepare" && state.statementCloses.Load() != 1 || operation == "begin" && state.rollbacks.Load() != 1 {
				t.Fatal("pool cleanup repeated canceled-result cleanup")
			}
		})
	}
}
