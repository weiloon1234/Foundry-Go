package expr_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type thresholdDriver struct {
	calls int
	sql   string
	args  []driver.NamedValue
	err   error
}

func (d *thresholdDriver) Driver() driver.Driver                        { return d }
func (d *thresholdDriver) Open(string) (driver.Conn, error)             { return &thresholdConn{d}, nil }
func (d *thresholdDriver) Connect(context.Context) (driver.Conn, error) { return d.Open("") }

type thresholdConn struct{ d *thresholdDriver }

func (c *thresholdConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *thresholdConn) Close() error               { return nil }
func (c *thresholdConn) Ping(context.Context) error { return nil }
func (c *thresholdConn) Begin() (driver.Tx, error)  { return thresholdTx{}, nil }
func (c *thresholdConn) ExecContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Result, error) {
	c.d.calls++
	c.d.sql, c.d.args = sql, append([]driver.NamedValue(nil), args...)
	return driver.RowsAffected(1), c.d.err
}

type thresholdTx struct{}

func (thresholdTx) Commit() error   { return nil }
func (thresholdTx) Rollback() error { return nil }

func TestTrigramThresholdValidationAndBoundSQL(t *testing.T) {
	d := &thresholdDriver{}
	db, err := database.Open(t.Context(), database.Adapter{Connector: d}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	for _, tx := range []*database.Tx{nil, {}} {
		if err := expr.SetTrigramThreshold(t.Context(), tx, 0.6); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid transaction accepted", err)
		}
	}
	var escaped *database.Tx
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		escaped = tx
		for _, invalid := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, 1e-310, math.Nextafter(0x1p-1022, 0)} {
			before := d.calls
			if err := expr.SetTrigramThreshold(t.Context(), tx, invalid); !errors.Is(err, fault.Invalid) || d.calls != before {
				t.Fatal("invalid threshold reached I/O", err, d.calls)
			}
		}
		for _, valid := range []struct {
			value float64
			bound string
		}{{0, "0"}, {math.Copysign(0, -1), "0"}, {0x1p-1022, "2.2250738585072014e-308"}, {1e-100, "1e-100"}, {0.6, "0.6"}, {1, "1"}} {
			before := d.calls
			if err := expr.SetTrigramThreshold(t.Context(), tx, valid.value); err != nil {
				return err
			}
			if d.calls != before+1 || d.sql != "SELECT set_config('pg_trgm.similarity_threshold', $1, true)" || !reflect.DeepEqual(d.args, []driver.NamedValue{{Ordinal: 1, Value: valid.bound}}) {
				t.Fatal("threshold did not bind local setting", d.sql, d.args)
			}
		}
		failure := errors.New("test executor failure")
		d.err = failure
		if err := expr.SetTrigramThreshold(t.Context(), tx, 0.6); !errors.Is(err, failure) {
			t.Fatal("lost executor failure", err)
		}
		d.err = nil
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := d.calls
	if err := expr.SetTrigramThreshold(t.Context(), escaped, 0.6); !errors.Is(err, fault.Invalid) || d.calls != before {
		t.Fatal("ended transaction reached I/O", err)
	}
}
