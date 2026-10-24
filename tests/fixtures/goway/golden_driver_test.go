package goway_test

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/model"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
)

const orderID = "01890f47-1a00-7000-8000-000000000001"
const buyerID = "01890f47-1a00-7000-8000-000000000002"

type connector struct{ capture *goldenCapture }

func (c connector) Driver() driver.Driver { return c }
func (c connector) Open(string) (driver.Conn, error) {
	return &connection{goldenCapture: c.capture}, nil
}
func (c connector) Connect(context.Context) (driver.Conn, error) {
	return &connection{goldenCapture: c.capture}, nil
}

type connection struct{ *goldenCapture }

func (*connection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (*connection) Close() error                        { return nil }
func (*connection) Begin() (driver.Tx, error)           { return transaction{}, nil }
func (*connection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return transaction{}, nil
}
func (*connection) Ping(context.Context) error { return nil }

type transaction struct{}

func (transaction) Commit() error   { return nil }
func (transaction) Rollback() error { return nil }

type goldenCapture struct {
	queryError func(string) error
	classify   func(error) database.Detail
	reply      func(string, []driver.NamedValue) ([]string, [][]driver.Value)
	onClose    func()

	quiet           bool // Benchmarks count real driver calls without formatting golden output.
	detector        *database.StatementDetector
	scopeContext    context.Context
	output          bytes.Buffer
	statements      int
	generatedKey    bool
	introducerKey   bool
	chunkBatches    [][][]driver.Value
	scriptedColumns []string
	scriptedBatches [][][]driver.Value
}
type oldGolden struct{ *goldenCapture }
type newGolden struct{ *goldenCapture }
type orderSide struct{}
type memberSide struct{}
type EmailRow struct{ Email string }

func (g *goldenCapture) printSQL(s string, args []driver.NamedValue) {
	g.statements++
	if g.quiet {
		return
	}
	values := make([]any, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	if g.generatedKey && strings.HasPrefix(s, "INSERT") {
		id, err := model.ParseID[struct{}](values[0].(string))
		check(err)
		bits := id.Bytes()
		if id.IsZero() || bits[6]>>4 != 7 || bits[8]>>6 != 2 {
			panic("missing generated UUIDv7")
		}
		values[0] = "<generated UUID>"
	}
	fmt.Fprintf(&g.output, "%s\n%#v\n", s, values)
}
func (g *connection) ExecContext(_ context.Context, s string, args []driver.NamedValue) (driver.Result, error) {
	g.printSQL(s, args)
	return driver.RowsAffected(1), nil
}
func (g *connection) QueryContext(_ context.Context, s string, args []driver.NamedValue) (driver.Rows, error) {
	g.printSQL(s, args)
	if g.queryError != nil {
		if err := g.queryError(s); err != nil {
			return nil, err
		}
	}
	if g.reply != nil {
		columns, batch := g.reply(s, args)
		return &rows{columns: columns, batch: batch, onClose: g.onClose}, nil
	}
	if g.scriptedColumns != nil {
		if len(g.scriptedBatches) == 0 {
			return nil, errors.New("unexpected scripted read")
		}
		batch := g.scriptedBatches[0]
		g.scriptedBatches = g.scriptedBatches[1:]
		return &rows{columns: g.scriptedColumns, batch: batch}, nil
	}
	if g.chunkBatches != nil && strings.Contains(s, `FROM "orders"`) {
		if len(g.chunkBatches) == 0 {
			panic("unexpected extra chunk read")
		}
		batch := g.chunkBatches[0]
		g.chunkBatches = g.chunkBatches[1:]
		return &rows{columns: []string{"id", "buyer_id", "total_cents"}, batch: batch}, nil
	}
	if strings.HasPrefix(s, "INSERT") && strings.Contains(s, `"orders"`) {
		return &rows{columns: []string{"id", "buyer_id", "total_cents"}, values: []driver.Value{orderID, buyerID, int64(500)}, remaining: 1 + strings.Count(s, "), (")}, nil
	}
	switch {
	case strings.Contains(s, `AS "email"`):
		return &rows{columns: []string{"email"}, values: []driver.Value{"a@example.test"}}, nil
	case strings.Contains(s, "INNER JOIN"):
		return &rows{columns: []string{"id", "member_name", "label", "amount"}, values: []driver.Value{orderID, "Ada", "Book", "12.50"}}, nil
	case strings.Contains(s, `AS "value"`):
		return &rows{columns: []string{"value"}, values: []driver.Value{int64(500)}}, nil
	case strings.Contains(s, `"report_members"`):
		return &rows{columns: []string{"id", "tenant_id", "name", "nickname", "state", "balance", "deleted_at"}, values: []driver.Value{buyerID, int64(7), "Ada", nil, "active", "42", nil}}, nil
	case strings.Contains(s, `"report_orders"`):
		var deleted driver.Value
		if strings.HasPrefix(s, "UPDATE") && len(args) > 0 {
			deleted = args[0].Value
		}
		return &rows{columns: []string{"id", "tenant_id", "member_id", "label", "amount", "deleted_at"}, values: []driver.Value{orderID, int64(7), buyerID, "Book", "12.50", deleted}}, nil
	case strings.HasPrefix(s, "SELECT COUNT"):
		return &rows{columns: []string{"count"}, values: []driver.Value{int64(1)}}, nil
	case strings.HasPrefix(s, "SELECT EXISTS"):
		return &rows{columns: []string{"exists"}, values: []driver.Value{true}}, nil
	case strings.Contains(s, `"write_records"`):
		return &rows{columns: []string{"code", "name", "enabled", "note", "amount"}, values: []driver.Value{int64(42), "default", true, nil, "7.25"}}, nil
	case strings.Contains(s, `"users"`):
		var introducer driver.Value
		if g.introducerKey {
			introducer = buyerID
		}
		return &rows{columns: []string{"id", "email_address", "age", "nickname", "status", "level", "birthday", "introducer_id"}, values: []driver.Value{buyerID, "a@example.test", int64(20), nil, "active", int64(1), nil, introducer}}, nil
	case strings.Contains(s, `"ledger_entries"`):
		return &rows{columns: []string{"id", "amount", "balance"}, values: []driver.Value{orderID, "12.50", nil}}, nil
	default:
		return &rows{columns: []string{"id", "buyer_id", "total_cents"}, values: []driver.Value{orderID, buyerID, int64(500)}}, nil
	}
}

type rows struct {
	onClose func()

	batch     [][]driver.Value
	index     int
	columns   []string
	values    []driver.Value
	done      bool
	remaining int
}

func (r *rows) Columns() []string { return r.columns }
func (r *rows) Close() error {
	if r.onClose != nil {
		r.onClose()
		r.onClose = nil
	}
	return nil
}
func (r *rows) Next(dest []driver.Value) error {
	if r.batch != nil {
		if r.index >= len(r.batch) {
			return io.EOF
		}
		copy(dest, r.batch[r.index])
		r.index++
		return nil
	}
	if r.done {
		return io.EOF
	}
	if r.remaining > 1 {
		r.remaining--
	} else {
		r.done = true
	}
	copy(dest, r.values)
	return nil
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
func (g *goldenCapture) open(ctx context.Context) *database.DB {
	options := []database.Option{database.WithClock(fixedClock{})}
	if g.detector != nil {
		options = append(options, database.WithStatementDetector(g.detector))
	}
	return must(database.Open(ctx, database.Adapter{Connector: connector{capture: g}, Classify: g.classify}, database.DefaultPoolConfig(), options...))
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// Compare persisted fields; Stage 1b prints each requested loaded slot explicitly.
// Get exposes Optional/Nullable presence; no private representation is printed.
func (g *goldenCapture) show(v any) {
	fmt.Fprintf(&g.output, "value: %+v\n", persisted(reflect.ValueOf(v)))
}
func persisted(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Slice {
		items := make([]any, v.Len())
		for i := range items {
			items[i] = persisted(v.Index(i))
		}
		return items
	}
	if v.Kind() != reflect.Struct {
		return v.Interface()
	}
	if strings.HasPrefix(v.Type().PkgPath(), "github.com/weiloon1234/Foundry-Go/value") {
		get := v.MethodByName("Get")
		if get.IsValid() {
			parts := get.Call(nil)
			return struct {
				Present bool
				Value   any
			}{parts[1].Bool(), persisted(parts[0])}
		}
	}
	if _, ok := v.Interface().(fmt.Stringer); ok {
		return v.Interface()
	}
	fields := []reflect.StructField{}
	values := []reflect.Value{}
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() || f.Tag.Get("foundry") == "-" || f.Type.PkgPath() == "github.com/weiloon1234/Foundry-Go/database/relation" {
			continue
		}
		fields = append(fields, reflect.StructField{Name: f.Name, Type: reflect.TypeFor[any]()})
		values = append(values, reflect.ValueOf(persisted(v.Field(i))))
	}
	result := reflect.New(reflect.StructOf(fields)).Elem()
	for i, field := range values {
		if field.IsValid() {
			result.Field(i).Set(field)
		}
	}
	return result.Interface()
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) }

func (g *goldenCapture) context() context.Context {
	if g.scopeContext != nil {
		return g.scopeContext
	}
	return context.Background()
}
