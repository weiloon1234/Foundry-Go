package datatable

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type accountRecord struct {
	ID       int64
	Username string
	Email    string
}

type accountColumns struct {
	orm.Table[accountRecord, int64]
	ID       orm.Primary[accountRecord, int64]
	Username orm.Text[accountRecord, string]
	Email    orm.Text[accountRecord, string]
}

var accountModels = orm.Define("accounts", func(b orm.Builder[accountRecord], r *accountRecord) accountColumns {
	table := orm.TableOf(b, &r.ID)
	return accountColumns{table, orm.PrimaryOf(b, table.Key()), orm.TextOf(b, &r.Username), orm.TextOf(b, &r.Email)}
})

type AccountRow struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

func accountJSON() contract.JSON[AccountRow] {
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.AccountRow"
	return contract.DefineJSON[AccountRow](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "username", Type: "text", Required: true}, {Name: "email", Type: "text", Required: true},
		}},
		{ID: "text", Kind: contract.StringKind},
	}})
}

func accountTable(columns ...ColumnRegistration[accountRecord, AccountRow]) Table[accountRecord, AccountRow, struct{}] {
	return Define(Spec[accountRecord, AccountRow, struct{}]{
		ID:        "reports.accounts",
		Row:       accountJSON(),
		Columns:   columns,
		Stable:    []expr.ProjectionOrder[accountRecord]{expr.Asc(expr.Value(accountModels.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source: func(context.Context, struct{}) (rows.SelectionSource[accountRecord], error) {
			return rows.From(accountModels.Query()), nil
		},
	})
}

// TestBindingMustAddressItsNamedField shows Define rejects a hand-written binding
// whose name is not the JSON property of the struct field its pointer addresses,
// even when both names are real properties of the same type.
func TestBindingMustAddressItsNamedField(t *testing.T) {
	m := accountModels
	username := validation.DefineBinding("username", func(r *AccountRow) *string { return &r.Username })
	email := validation.DefineBinding("email", func(r *AccountRow) *string { return &r.Email })
	if err := accountTable(Text(username, "fields.username", m.Username), Text(email, "fields.email", m.Email)).Validate(); err != nil {
		t.Fatalf("matching bindings rejected: %v", err)
	}
	for label, tc := range map[string]struct {
		table      Table[accountRecord, AccountRow, struct{}]
		name, json string
	}{
		"displayed": {
			table: accountTable(
				Text(validation.DefineBinding("email", func(r *AccountRow) *string { return &r.Username }), "fields.email", m.Email),
				Text(validation.DefineBinding("username", func(r *AccountRow) *string { return &r.Email }), "fields.username", m.Username),
			),
			name: "email", json: "username",
		},
		"hidden": {
			table: accountTable(Text(username, "fields.username", m.Username), Hidden(validation.DefineBinding("contact", func(r *AccountRow) *string { return &r.Email }), m.Email)),
			name:  "contact", json: "email",
		},
	} {
		err := tc.table.Validate()
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), fmt.Sprintf("%q", tc.name)) || !strings.Contains(err.Error(), fmt.Sprintf("%q", tc.json)) {
			t.Fatalf("%s: err = %v, want a declaration error naming %q and %q", label, err, tc.name, tc.json)
		}
	}
}

// TestDynamicSlotsBindExactlyOnce covers the slot bookkeeping Define performs
// without invoking any builder.
func TestDynamicSlotsBindExactlyOnce(t *testing.T) {
	m := accountModels
	username := validation.DefineBinding("username", func(r *AccountRow) *string { return &r.Username })
	email := validation.DefineBinding("email", func(r *AccountRow) *string { return &r.Email })
	slot, other := NewDynamicSlot[accountRecord, string](), NewDynamicSlot[accountRecord, string]()
	build := func(context.Context, struct{}) expr.Selected[accountRecord, string] {
		t.Fatal("Define invoked a dynamic builder")
		return expr.Selected[accountRecord, string]{}
	}
	define := func(columns []ColumnRegistration[accountRecord, AccountRow], dynamic ...DynamicBinding[accountRecord, struct{}]) error {
		return Define(Spec[accountRecord, AccountRow, struct{}]{
			ID:        "reports.accounts",
			Row:       accountJSON(),
			Columns:   columns,
			Dynamic:   dynamic,
			Stable:    []expr.ProjectionOrder[accountRecord]{expr.Asc(expr.Value(m.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source: func(context.Context, struct{}) (rows.SelectionSource[accountRecord], error) {
				return rows.From(m.Query()), nil
			},
		}).Validate()
	}
	columns := []ColumnRegistration[accountRecord, AccountRow]{DynamicColumn(username, "fields.username", slot), Text(email, "fields.email", m.Email)}
	if err := define(columns, BindDynamic(slot, build)); err != nil {
		t.Fatalf("bound slot rejected: %v", err)
	}
	for label, err := range map[string]error{
		"unbound":     define(columns),
		"stray":       define(columns, BindDynamic(slot, build), BindDynamic(other, build)),
		"bound twice": define(columns, BindDynamic(slot, build), BindDynamic(slot, build)),
		"foreign":     define(columns, BindDynamic(other, build)),
		"nil builder": define(columns, BindDynamic[accountRecord, struct{}](slot, nil)),
		"zero slot":   define([]ColumnRegistration[accountRecord, AccountRow]{DynamicColumn(username, "fields.username", DynamicSlot[accountRecord, string]{}), Text(email, "fields.email", m.Email)}),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s: err = %v, want fault.Invalid", label, err)
		}
	}
	repeated := []ColumnRegistration[accountRecord, AccountRow]{DynamicColumn(username, "fields.username", slot), DynamicColumn(email, "fields.email", slot)}
	if err := define(repeated, BindDynamic(slot, build)); !errors.Is(err, fault.Duplicate) {
		t.Fatalf("repeated slot: err = %v, want fault.Duplicate", err)
	}
}

// echoDriver answers every query from memory and records each statement it
// receives. A count query returns total; any other query returns one row built
// from its arguments.
type echoDriver struct {
	columns []string
	row     func(args []any) []driver.Value
	total   int64
	mu      sync.Mutex
	queries []echoQuery
}

type echoQuery struct {
	SQL       string
	Arguments []any
}

// allKindsEcho answers with one all-kinds row whose name is the query's first
// text argument, so a dynamic constant selection shows which request it came from.
func allKindsEcho() *echoDriver {
	at := time.Date(2026, 9, 25, 17, 30, 0, 0, time.UTC)
	return &echoDriver{
		columns: []string{"name", "count", "created_at", "status", "active", "nickname", "discount", "paid_at", "opt_status"},
		total:   1,
		row: func(args []any) []driver.Value {
			name := "no text argument"
			for _, arg := range args {
				if text, ok := arg.(string); ok {
					name = text
					break
				}
			}
			return []driver.Value{name, int64(1), at, "active", true, nil, "12.50", at, "active"}
		},
	}
}

func (d *echoDriver) Driver() driver.Driver                        { return d }
func (d *echoDriver) Open(string) (driver.Conn, error)             { return echoConn{d}, nil }
func (d *echoDriver) Connect(context.Context) (driver.Conn, error) { return echoConn{d}, nil }

// recorded returns and clears the statements received so far.
func (d *echoDriver) recorded() []echoQuery {
	d.mu.Lock()
	defer d.mu.Unlock()
	queries := d.queries
	d.queries = nil
	return queries
}

type echoConn struct{ d *echoDriver }

func (echoConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (echoConn) Close() error                        { return nil }
func (echoConn) Begin() (driver.Tx, error)           { return exportBenchTx{}, nil }
func (echoConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return exportBenchTx{}, nil
}
func (echoConn) Ping(context.Context) error { return nil }
func (echoConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (c echoConn) QueryContext(_ context.Context, query string, named []driver.NamedValue) (driver.Rows, error) {
	args := make([]any, len(named))
	for i, arg := range named {
		args[i] = arg.Value
	}
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, echoQuery{SQL: query, Arguments: args})
	c.d.mu.Unlock()
	if strings.HasPrefix(query, "SELECT COUNT(*)") {
		return &echoRows{columns: []string{"count"}, row: []driver.Value{c.d.total}}, nil
	}
	return &echoRows{columns: c.d.columns, row: c.d.row(args)}, nil
}

type echoRows struct {
	columns []string
	row     []driver.Value
	done    bool
}

func (r *echoRows) Columns() []string { return r.columns }
func (r *echoRows) Close() error      { return nil }
func (r *echoRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}

type localeActor struct{ locale string }

// localizedTable selects its name per request from the actor's locale.
func localizedTable(build func(context.Context, localeActor) expr.Selected[allKindsRecord, string]) Table[allKindsRecord, AllKindsRow, localeActor] {
	f := allKindsModels
	w := allKindsBindings()
	slot := NewDynamicSlot[allKindsRecord, string]()
	return Define(Spec[allKindsRecord, AllKindsRow, localeActor]{
		ID:      "reports.localized",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			DynamicColumn(w.Name, "fields.name", slot, SortBy(expr.AsSelected(expr.Row(expr.Value(f.Name)))), ExportAs(expr.AsSelected(expr.Row(expr.Value(f.Name))), ScalarCell(foundryhttp.StringQuery[string]()))),
			Number(w.Count, "fields.count", f.Count, foundryhttp.IntegerQuery[int64]()),
			Hidden(w.CreatedAt, f.CreatedAt),
			Hidden(w.Status, f.Status),
			Hidden(w.Active, f.Active),
			Hidden(w.Nickname, f.Nickname),
			Hidden(w.Discount, f.Discount),
			Hidden(w.PaidAt, f.PaidAt),
			Hidden(w.OptStatus, f.OptStatus),
		},
		Dynamic:   []DynamicBinding[allKindsRecord, localeActor]{BindDynamic(slot, build)},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, localeActor, Action) error { return nil },
		Source: func(context.Context, localeActor) (rows.SelectionSource[allKindsRecord], error) {
			return rows.From(f.Query()), nil
		},
	})
}

func localeConstant(_ context.Context, a localeActor) expr.Selected[allKindsRecord, string] {
	return expr.AsSelected(expr.Constant[allKindsRecord](codec.String[string](), a.locale))
}

func echoManager(t *testing.T, drv *echoDriver, registrations ...Registration) *Manager {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: drv}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	locales, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.TempDir = t.TempDir()
	labels := func(_ context.Context, _ i18n.LocaleID, key i18n.MessageKey) (string, error) { return string(key), nil }
	manager, err := New(Dependencies{Database: db, Locales: locales, Labels: labels}, config, registrations...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager
}

// TestDynamicBuilderRunsOncePerRequest pins when a BindDynamic builder runs:
// never while a table is declared, validated, described or inspected, and
// exactly once for each query, count and export request.
func TestDynamicBuilderRunsOncePerRequest(t *testing.T) {
	var calls atomic.Int64
	table := localizedTable(func(ctx context.Context, a localeActor) expr.Selected[allKindsRecord, string] {
		calls.Add(1)
		return localeConstant(ctx, a)
	})
	expect := func(step string, want int64) {
		t.Helper()
		if got := calls.Load(); got != want {
			t.Fatalf("after %s: builder calls = %d, want %d", step, got, want)
		}
	}
	expect("Define", 0)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	expect("Validate", 0)
	if _, err := table.Description(); err != nil {
		t.Fatal(err)
	}
	expect("Description", 0)
	manager := echoManager(t, allKindsEcho(), table.Registration())
	actor := localeActor{locale: "ms"}
	if _, err := table.Inspect(t.Context(), manager, actor); err != nil {
		t.Fatal(err)
	}
	expect("Inspect", 0)
	page, err := table.Query(t.Context(), manager, actor, Request{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "ms" {
		t.Fatalf("Query = %+v, %v", page, err)
	}
	expect("Query", 1)
	if _, err := table.SimpleQuery(t.Context(), manager, actor, Request{}); err != nil {
		t.Fatal(err)
	}
	expect("SimpleQuery", 2)
	if total, err := table.Count(t.Context(), manager, actor, Request{}); err != nil || total != 1 {
		t.Fatalf("Count = %d, %v", total, err)
	}
	expect("Count", 3)
	artifact, err := table.Export(t.Context(), manager, actor, Request{}, ExportOptions{Format: CSV, Presentation: Presentation{Locale: "en", TimeZone: "UTC"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(artifact)
	if closeErr := artifact.Close(); err != nil || closeErr != nil || !strings.Contains(string(data), "ms") {
		t.Fatalf("Export = %q, %v, %v", data, err, closeErr)
	}
	expect("Export", 4)
}

// TestDynamicValuesStayWithTheirRequest shares one defined table across many
// concurrent requests with different per-request values; every decoded row must
// carry its own request's value. Run under -race to check the shared plan.
func TestDynamicValuesStayWithTheirRequest(t *testing.T) {
	table := localizedTable(localeConstant)
	manager := echoManager(t, allKindsEcho(), table.Registration())
	const workers, requests = 8, 16
	var wg sync.WaitGroup
	failures := make(chan string, workers*requests)
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for request := range requests {
				locale := fmt.Sprintf("locale-%d-%d", worker, request)
				page, err := table.Query(t.Context(), manager, localeActor{locale: locale}, Request{Search: ""})
				if err != nil {
					failures <- fmt.Sprintf("%s: %v", locale, err)
					continue
				}
				if len(page.Items) != 1 || page.Items[0].Name != locale {
					failures <- fmt.Sprintf("%s: got rows %+v", locale, page.Items)
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

type brokenEnum string

func (brokenEnum) EnumDescriptor() enum.Descriptor[brokenEnum] { return enum.Descriptor[brokenEnum]{} }

type brokenEnumRecord struct {
	ID     int64
	Status brokenEnum
}

type brokenEnumColumns struct {
	orm.Table[brokenEnumRecord, int64]
	ID     orm.Primary[brokenEnumRecord, int64]
	Status orm.Enum[brokenEnumRecord, brokenEnum]
}

type BrokenEnumRow struct {
	ID     int64      `json:"id"`
	Status brokenEnum `json:"status"`
}

// TestScanWithKeepsTheColumnEnumCheck shows a custom scan codec on a column whose
// enum declaration is invalid fails the table declaration, as the column's own
// codec does.
func TestScanWithKeepsTheColumnEnumCheck(t *testing.T) {
	m := orm.Define("broken_enums", func(b orm.Builder[brokenEnumRecord], r *brokenEnumRecord) brokenEnumColumns {
		table := orm.TableOf(b, &r.ID)
		return brokenEnumColumns{table, orm.PrimaryOf(b, table.Key()), orm.EnumOf(b, &r.Status)}
	})
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.BrokenEnumRow"
	row := contract.DefineJSON[BrokenEnumRow](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "integer", Required: true}, {Name: "status", Type: "text", Required: true}}},
		{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		{ID: "text", Kind: contract.StringKind},
	}})
	id := validation.DefineBinding("id", func(r *BrokenEnumRow) *int64 { return &r.ID })
	status := validation.DefineBinding("status", func(r *BrokenEnumRow) *brokenEnum { return &r.Status })
	selected := expr.AsSelected(expr.Row(expr.Value(m.Status)))
	for _, options := range [][]ColumnOption[brokenEnumRecord, brokenEnum]{nil, {ScanWith[brokenEnumRecord](orm.Codec(codec.String[brokenEnum]()))}} {
		err := Define(Spec[brokenEnumRecord, BrokenEnumRow, struct{}]{
			ID:        "reports.broken_enums",
			Row:       row,
			Columns:   []ColumnRegistration[brokenEnumRecord, BrokenEnumRow]{Column(id, "fields.id", expr.AsSelected(expr.Row(expr.Value(m.ID)))), HiddenSelected(status, selected, options...)},
			Stable:    []expr.ProjectionOrder[brokenEnumRecord]{expr.Asc(expr.Value(m.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source: func(context.Context, struct{}) (rows.SelectionSource[brokenEnumRecord], error) {
				return rows.From(m.Query()), nil
			},
		}).Validate()
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "invalid enum declaration") {
			t.Fatalf("ScanWith options %d: err = %v, want the enum declaration error", len(options), err)
		}
	}
}
