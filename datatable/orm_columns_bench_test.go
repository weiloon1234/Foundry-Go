package datatable

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// BenchmarkDirectColumnSpecConstruction measures complete table construction
// (columns, metadata and the query plan) across the 9 direct column constructors.
func BenchmarkDirectColumnSpecConstruction(b *testing.B) {
	f := allKindsModels
	w := allKindsBindings()
	rowJSON := allKindsJSON()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	b.ReportAllocs()
	for b.Loop() {
		table := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
			ID:      "reports.all_kinds",
			Row:     rowJSON,
			Exports: true,
			Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
				Text(w.Name, "fields.name", f.Name),
				Number(w.Count, "fields.count", f.Count, intCodec),
				Time(w.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec),
				Enum(w.Status, "fields.status", f.Status, enumCodec),
				Bool(w.Active, "fields.active", f.Active),
				NullableText(w.Nickname, "fields.nickname", f.Nickname),
				NullableNumber(w.Discount, "fields.discount", f.Discount, decCodec),
				NullableTime(w.PaidAt, "fields.paid_at", f.PaidAt, dtCodec),
				NullableEnum(w.OptStatus, "fields.opt_status", f.OptStatus, enumCodec),
			},
			Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source:    allKindsBase,
		})
		if err := table.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDirectColumnQueryCompile measures per-request query compilation
// (prepare, scoped plan query and SQL statement) for the 9 direct columns.
func BenchmarkDirectColumnQueryCompile(b *testing.B) {
	table := allKindsBenchTable("reports.all_kinds", false)
	req := Request{
		Search:  "Ada",
		Sort:    []Sort{{Column: "created_at", Direction: Descending}},
		Filters: []Filter{{Column: "count", Op: Greater, Values: []string{"5"}}},
	}
	cfg := DefaultConfig()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		prep, err := table.definition.prepare(req, cfg)
		if err != nil {
			b.Fatal(err)
		}
		scoped, err := table.definition.scoped(ctx, struct{}{}, QueryAction, prep)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := rows.CompileRead(rows.Read(scoped.rows)); err != nil {
			b.Fatal(err)
		}
	}
}

// allKindsBenchTable declares the 9 direct columns; exportOnly removes sort and
// filter so an export measures row scanning and cell formatting.
func allKindsBenchTable(id contract.TableID, exportOnly bool) Table[allKindsRecord, AllKindsRow, struct{}] {
	f := allKindsModels
	w := allKindsBindings()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())
	var text, other []ColumnFeature
	if exportOnly {
		text, other = []ColumnFeature{NoSort, NoFilter, NoSearch}, []ColumnFeature{NoSort, NoFilter}
	}
	return Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      id,
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(w.Name, "fields.name", f.Name, text...),
			Number(w.Count, "fields.count", f.Count, intCodec, other...),
			Time(w.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec, other...),
			Enum(w.Status, "fields.status", f.Status, enumCodec, other...),
			Bool(w.Active, "fields.active", f.Active, other...),
			NullableText(w.Nickname, "fields.nickname", f.Nickname, text...),
			NullableNumber(w.Discount, "fields.discount", f.Discount, decCodec, other...),
			NullableTime(w.PaidAt, "fields.paid_at", f.PaidAt, dtCodec, other...),
			NullableEnum(w.OptStatus, "fields.opt_status", f.OptStatus, enumCodec, other...),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
}

// exportBenchDriver answers the export transaction's search_path command and
// returns the same rows for every query; it records nothing per row.
type exportBenchDriver struct {
	columns []string
	rows    [][]driver.Value
}

func (d *exportBenchDriver) Driver() driver.Driver                        { return d }
func (d *exportBenchDriver) Open(string) (driver.Conn, error)             { return exportBenchConn{d}, nil }
func (d *exportBenchDriver) Connect(context.Context) (driver.Conn, error) { return d.Open("") }

type exportBenchConn struct{ d *exportBenchDriver }

func (exportBenchConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (exportBenchConn) Close() error              { return nil }
func (exportBenchConn) Begin() (driver.Tx, error) { return exportBenchTx{}, nil }
func (exportBenchConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return exportBenchTx{}, nil
}
func (exportBenchConn) Ping(context.Context) error { return nil }
func (exportBenchConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (c exportBenchConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &exportBenchRows{d: c.d}, nil
}

type exportBenchTx struct{}

func (exportBenchTx) Commit() error   { return nil }
func (exportBenchTx) Rollback() error { return nil }

type exportBenchRows struct {
	d     *exportBenchDriver
	index int
}

func (r *exportBenchRows) Columns() []string { return r.d.columns }
func (r *exportBenchRows) Close() error      { return nil }
func (r *exportBenchRows) Next(dest []driver.Value) error {
	if r.index >= len(r.d.rows) {
		return io.EOF
	}
	copy(dest, r.d.rows[r.index])
	r.index++
	return nil
}

// BenchmarkTableExport drives the real Table.Export path (authorization, scoped
// query, rows.EachRead scanning, column cell bindings, formatting and the CSV/XLSX
// writer). Two row counts give the marginal allocations and bytes per exported
// row: (allocs/op(1000) - allocs/op(100)) / 900.
func BenchmarkTableExport(b *testing.B) {
	table := allKindsBenchTable("reports.export", true)
	ts := time.Date(2026, 9, 25, 17, 30, 0, 0, time.UTC)
	drv := &exportBenchDriver{columns: []string{"name", "count", "created_at", "status", "active", "nickname", "discount", "paid_at", "opt_status"}}
	db, err := database.Open(context.Background(), database.Adapter{Connector: drv}, database.DefaultPoolConfig())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close(context.Background()) })
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		b.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.TempDir = b.TempDir()
	labels := func(_ context.Context, _ i18n.LocaleID, key i18n.MessageKey) (string, error) { return string(key), nil }
	mgr, err := New(Dependencies{Database: db, Locales: locales, Labels: labels}, cfg, table.Registration())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = mgr.Close(context.Background()) })

	ctx := context.Background()
	for _, format := range []ExportFormat{CSV, XLSX} {
		for _, rowCount := range []int{100, 1000} {
			b.Run(fmt.Sprintf("%s/rows=%d", format, rowCount), func(b *testing.B) {
				drv.rows = make([][]driver.Value, rowCount)
				for i := range drv.rows {
					drv.rows[i] = []driver.Value{"Ada Lovelace", int64(i + 1), ts, "active", true, "ada", "12.50", ts, "active"}
				}
				opts := ExportOptions{Format: format, Presentation: Presentation{Locale: "en", TimeZone: "UTC"}}
				b.ReportAllocs()
				for b.Loop() {
					art, err := table.Export(ctx, mgr, struct{}{}, Request{}, opts)
					if err != nil {
						b.Fatal(err)
					}
					if art.Rows() != rowCount {
						b.Fatalf("exported %d rows, want %d", art.Rows(), rowCount)
					}
					if err := art.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
