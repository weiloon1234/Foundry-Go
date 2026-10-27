package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type batchDriver struct {
	testConnector
	mode   string
	native bool
	failed atomic.Bool
	active atomic.Int32
	exited atomic.Int32
	cause  *hostileQueryCallbackError
}

func (d *batchDriver) Connect(context.Context) (driver.Conn, error) {
	return batchConnection{testConnection{d.testConnector}, d}, nil
}

type batchConnection struct {
	testConnection
	driver *batchDriver
}

func (c batchConnection) QueryContext(ctx context.Context, s string, args []driver.NamedValue) (driver.Rows, error) {
	source, err := c.testConnection.QueryContext(ctx, s, args)
	if err != nil {
		return nil, err
	}
	rows := &batchDriverRows{testDriverRows: source.(*testDriverRows), driver: c.driver}
	if c.driver.native {
		return &nativeBatchDriverRows{rows}, nil
	}
	return rows, nil
}

type batchDriverRows struct {
	*testDriverRows
	driver *batchDriver
}

func (r *batchDriverRows) fail() error {
	r.driver.active.Add(1)
	defer r.driver.active.Add(-1)
	defer r.driver.exited.Add(1)
	if r.driver.failed.Load() || r.index != 1 {
		return nil
	}
	switch r.driver.mode {
	case "panic":
		panic(r.driver.cause)
	case "exit":
		runtime.Goexit()
	case "error":
		return r.driver.cause
	}
	return nil
}
func (r *batchDriverRows) Next(dst []driver.Value) error {
	if err := r.fail(); err != nil {
		return err
	}
	return r.testDriverRows.Next(dst)
}

type nativeBatchDriverRows struct{ *batchDriverRows }

func (r *nativeBatchDriverRows) NextRow() error {
	// ScanColumn failures exercise database/sql's non-deferred scan lock.
	if r.driver.mode != "scan_exit" {
		if err := r.fail(); err != nil {
			return err
		}
	}
	return r.testDriverRows.Next(make([]driver.Value, 2))
}
func (r *nativeBatchDriverRows) ScanColumn(ctx driver.ScanContext, index int, dst any) error {
	if r.driver.mode == "scan_exit" && !r.driver.failed.Load() && r.index == 2 && index == 1 {
		r.driver.active.Add(1)
		defer r.driver.active.Add(-1)
		defer r.driver.exited.Add(1)
		runtime.Goexit()
	}
	return sql.ConvertAssign(ctx, dst, r.values[r.index-1][index])
}

func TestCollectedDriverFailuresKeepContainmentAndReleaseSQLLocks(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, mode := range []string{"error", "panic", "exit", "scan_exit"} {
			if mode == "scan_exit" && !native {
				continue
			}
			t.Run(fmt.Sprintf("native=%v/%s", native, mode), func(t *testing.T) {
				closed := 0
				source := &batchDriver{native: native, mode: mode, cause: &hostileQueryCallbackError{mode: "exit"}, testConnector: testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "first"}, {int64(2), "second"}, {int64(3), "third"}}, onClose: func() { closed++ }}}
				db, err := database.Open(t.Context(), database.Adapter{Connector: source}, database.DefaultPoolConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				table := Define("batch_driver_records", func(b Builder[containedReadRow], v *containedReadRow) containedReadTable {
					return containedReadTable{Table: TableOf(b, &v.ID), Name: TextOf(b, &v.Name)}
				})
				items, err := table.Query().All(t.Context(), db)
				if err == nil || items != nil || source.active.Load() != 0 || source.exited.Load() == 0 || closed != 1 || db.Stats().InUse != 0 {
					t.Fatal("failed batch retained partial rows or SQL ownership")
				}
				if errors.Is(err, fault.Panicked) != (mode != "error") {
					t.Fatal("driver interruption classification changed")
				}
				if mode == "error" {
					var retained *hostileQueryCallbackError
					if !errors.Is(err, source.cause) || !errors.As(err, &retained) || retained != source.cause {
						t.Fatal("driver identity lost")
					}
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") || source.cause.calls != 0 {
					t.Fatal("driver error formatted")
				}
				source.failed.Store(true)
				items, err = table.Query().All(t.Context(), db)
				if err != nil || len(items) != 3 || items[0].Name != "first" || items[2].Name != "third" || db.Stats().InUse != 0 {
					t.Fatal("subsequent read could not reuse released capacity")
				}
				// The same driver interruption after an autocommit write is an
				// unknown outcome, never a successful/definitely rolled-back write.
				source.failed.Store(false)
				stream, err := db.FoundryAutocommitQuery(sqlowner.Autocommit(), t.Context(), "INSERT INTO records DEFAULT VALUES RETURNING id, name")
				if err != nil {
					t.Fatal(err)
				}
				count, err := stream.FoundryCollect(sqlowner.Collections(), t.Context(), func(row database.Row) error {
					var id int64
					var name string
					return row.Scan(&id, &name)
				})
				var outcome *database.Error
				if count != 0 || !errors.Is(err, database.CommitUnknown) || !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown || closed != 3 || db.Stats().InUse != 0 {
					t.Fatal("failed driver collection lost its unknown write outcome or stream ownership")
				}
			})
		}
	}
}
