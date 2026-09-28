package idempotency

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
)

// This test adapter uses the real PostgreSQL connector and changes only the
// acknowledgment returned from one owned outer Commit. Savepoints remain SQL.
type acknowledgmentConnector struct {
	driver.Connector
	armed    *atomic.Bool
	rollback bool
	metrics  *driverMetrics
}

func (c acknowledgmentConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &acknowledgmentConn{Conn: conn, armed: c.armed, rollback: c.rollback, metrics: c.metrics}, nil
}

type acknowledgmentConn struct {
	driver.Conn
	armed    *atomic.Bool
	rollback bool
	metrics  *driverMetrics
}

func (c *acknowledgmentConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if c.metrics != nil {
		c.metrics.commands.Add(1)
	}
	started := time.Now()
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &acknowledgmentTx{Tx: tx, armed: c.armed, rollback: c.rollback, metrics: c.metrics, started: started}, nil
}
func (c *acknowledgmentConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.metrics != nil {
		c.metrics.commands.Add(1)
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *acknowledgmentConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if c.metrics != nil {
		c.metrics.commands.Add(1)
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *acknowledgmentConn) ResetSession(ctx context.Context) error {
	if c.metrics != nil {
		c.metrics.resets.Add(1)
	}
	return c.Conn.(driver.SessionResetter).ResetSession(ctx)
}
func (c *acknowledgmentConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *acknowledgmentConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}
func (c *acknowledgmentConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

type acknowledgmentTx struct {
	driver.Tx
	armed    *atomic.Bool
	rollback bool
	metrics  *driverMetrics
	started  time.Time
}

func (tx *acknowledgmentTx) Commit() error {
	if tx.metrics != nil {
		tx.metrics.commands.Add(1)
		defer func() { tx.metrics.transactionNanos.Add(time.Since(tx.started).Nanoseconds()) }()
	}
	if tx.armed != nil && tx.armed.Swap(false) {
		if tx.rollback {
			if err := tx.Tx.Rollback(); err != nil {
				return err
			}
		} else {
			if err := tx.Tx.Commit(); err != nil {
				return err
			}
		}
		return errors.New("injected private commit acknowledgment failure")
	}
	return tx.Tx.Commit()
}
func TestUnknownCommitReconcilesPrimaryWithoutRepeatingCallback(t *testing.T) {
	scope, assertions := testDatabase(t)
	for _, rollback := range []bool{false, true} {
		name := "committed"
		if rollback {
			name = "rolled-back"
		}
		t.Run(name, func(t *testing.T) {
			config := scope.Config()
			adapter, err := postgres.New(config)
			if err != nil {
				t.Fatal(err)
			}
			var armed atomic.Bool
			adapter.Connector = acknowledgmentConnector{Connector: adapter.Connector, armed: &armed, rollback: rollback}
			db, err := database.Open(t.Context(), adapter, config.Pool)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := db.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			op := testOperation(t, testStore(t, db, scope.Schema(), nil), OperationID("ack."+name))
			identity, key := testScope(t, "ack"), testKey(t, "acknowledgment-"+name)
			var calls atomic.Int32
			handler := func(ctx context.Context, tx *database.Tx, in testInput) (testOutput, error) {
				calls.Add(1)
				return effect(ctx, tx, in)
			}
			armed.Store(true)
			result, err := op.Run(t.Context(), identity, key, testInput{name}, handler)
			if calls.Load() != 1 {
				t.Fatal("unknown commit repeated callback")
			}
			if rollback {
				if result.Committed() || !errors.Is(err, Unavailable) || countEffects(t, assertions, name) != 0 {
					t.Fatal("unknown rolled-back outcome was claimed successful", err)
				}
			} else {
				if !result.Committed() || !result.Replayed() || !errors.Is(err, database.CommitUnknown) || countEffects(t, assertions, name) != 1 {
					t.Fatal("committed acknowledgment did not reconcile", err)
				}
			}
			retry, err := op.Run(t.Context(), identity, key, testInput{name}, handler)
			if err != nil || !retry.Committed() || countEffects(t, assertions, name) != 1 {
				t.Fatal("same-key retry did not converge", err)
			}
			expected := int32(1)
			if rollback {
				expected = 2
			}
			if calls.Load() != expected {
				t.Fatal("committed work repeated")
			}
		})
	}
}

type driverMetrics struct{ commands, resets, transactionNanos atomic.Int64 }
