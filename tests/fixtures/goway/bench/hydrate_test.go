package bench

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/model"
	"io"
	"strings"
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

// Full public read paths over deterministic database/sql rows. Setup is excluded;
// SQL compilation, scanning, relation attachment, cursors and result checks are timed.
type allocDriver struct {
	users, orders [][]driver.Value
	queries       int
	cursor        bool
}

func (d *allocDriver) Driver() driver.Driver                        { return d }
func (d *allocDriver) Open(string) (driver.Conn, error)             { return d, nil }
func (d *allocDriver) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (d *allocDriver) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *allocDriver) Close() error               { return nil }
func (d *allocDriver) Begin() (driver.Tx, error)  { return nil, errors.New("unexpected transaction") }
func (d *allocDriver) Ping(context.Context) error { return nil }
func (d *allocDriver) QueryContext(_ context.Context, s string, _ []driver.NamedValue) (driver.Rows, error) {
	d.queries++
	if strings.Contains(s, `FROM "users"`) {
		return &allocRows{columns: []string{"id", "email_address", "age", "nickname", "status", "level", "birthday", "introducer_id"}, values: d.users}, nil
	}
	rows := d.orders
	if d.cursor {
		if strings.Contains(s, "WHERE") {
			rows = rows[4:9]
		} else {
			rows = rows[:5]
		}
	}
	return &allocRows{columns: []string{"id", "buyer_id", "total_cents"}, values: rows}, nil
}

type allocRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *allocRows) Columns() []string { return r.columns }
func (r *allocRows) Close() error      { return nil }
func (r *allocRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
func allocDB(b *testing.B, cursor bool) (*database.DB, *allocDriver) {
	b.Helper()
	d := &allocDriver{cursor: cursor}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("01890f47-1a00-7000-8000-%012d", i+1)
		d.users = append(d.users, []driver.Value{id, "a@example.test", int64(20), nil, "active", int64(1), nil, nil})
	}
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("01890f47-1a00-7000-8000-%012d", i+100)
		d.orders = append(d.orders, []driver.Value{id, d.users[0][0], int64(i)})
	}
	db, err := database.Open(context.Background(), database.Adapter{Connector: d}, database.DefaultPoolConfig())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close(context.Background()) })
	return db, d
}
func BenchmarkHydrate1000(b *testing.B) {
	db, d := allocDB(b, false)
	ctx := context.Background()
	q := models.Orders.Query()
	if _, err := q.All(ctx, db); err != nil {
		b.Fatal(err)
	}
	ids := make([]model.ID[models.Order], 1000)
	for i := range ids {
		var err error
		ids[i], err = model.ParseID[models.Order](d.orders[i][0].(string))
		if err != nil {
			b.Fatal(err)
		}
	}
	buyer, err := model.ParseID[models.User](d.users[0][0].(string))
	if err != nil {
		b.Fatal(err)
	}
	d.queries = 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := q.All(ctx, db)
		if err != nil || len(items) != 1000 {
			b.Fatalf("hydrate: %v rows=%d", err, len(items))
		}
		for index, item := range items {
			if item.TotalCents != int64(index) || item.ID != ids[index] || item.BuyerID != buyer {
				b.Fatal("incorrect hydration")
			}
		}
	}
	b.StopTimer()
	if d.queries != b.N {
		b.Fatalf("queries=%d", d.queries)
	}
	b.ReportMetric(1000, "rows/op")
	b.ReportMetric(1, "queries/op")
}
