package ormquerytest

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fixedNow struct{ instant time.Time }

func (c fixedNow) Now() time.Time { return c.instant }

type record struct {
	ID      int64  `foundry:"default=database"`
	Name    string `foundry:"column=display_name"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal
	Payload []byte `foundry:"-"`
	Related relation.One[record]
}
type recordTable struct {
	orm.Table[record, int64]
	ID     orm.Key[record, int64]
	Name   orm.Text[record, string]
	Note   orm.NullableText[record, string]
	Amount orm.Number[record, decimal.Decimal]
}

func records() recordTable {
	return orm.Define("records", func(b orm.Builder[record], r *record) recordTable {
		t := orm.TableOf(b, &r.ID)
		return recordTable{Table: t, ID: t.Key(), Name: orm.TextOf(b, &r.Name), Note: orm.NullableTextOf(b, &r.Note), Amount: orm.NumberOf(b, &r.Amount)}
	})
}

type testConnector struct {
	affected     *int64
	statements   *[]string
	bindings     *[][]any
	onClose      func()
	onBegin      func()
	values       [][]driver.Value
	columns      []string
	execError    func(string) error
	commitErr    error
	closeErr     error
	respondBound func(string, []driver.NamedValue) ([]string, [][]driver.Value, error)
	respond      func(string) ([]string, [][]driver.Value)
}

func (c testConnector) Connect(context.Context) (driver.Conn, error) { return testConnection{c}, nil }
func (c testConnector) Driver() driver.Driver                        { return c }
func (c testConnector) Open(string) (driver.Conn, error)             { return testConnection{c}, nil }

type testConnection struct{ testConnector }

func (c testConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c testConnection) Close() error { return nil }
func (c testConnection) Begin() (driver.Tx, error) {
	if c.onBegin != nil {
		c.onBegin()
	}
	return testTransaction{c.commitErr}, nil
}
func (c testConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c testConnection) Ping(context.Context) error { return nil }
func (c testConnection) ExecContext(_ context.Context, s string, _ []driver.NamedValue) (driver.Result, error) {
	if c.statements != nil {
		*c.statements = append(*c.statements, s)
	}
	if c.execError != nil {
		if err := c.execError(s); err != nil {
			return nil, err
		}
	}
	if c.affected != nil {
		return driver.RowsAffected(*c.affected), nil
	}
	return driver.RowsAffected(1), nil
}
func (c testConnection) QueryContext(_ context.Context, s string, args []driver.NamedValue) (driver.Rows, error) {
	if c.statements != nil {
		*c.statements = append(*c.statements, s)
	}
	if c.bindings != nil {
		values := make([]any, len(args))
		for i, a := range args {
			values[i] = a.Value
		}
		*c.bindings = append(*c.bindings, values)
	}
	if c.respondBound != nil {
		columns, values, err := c.respondBound(s, args)
		if err != nil {
			return nil, err
		}
		return &testDriverRows{columns: columns, values: values, closeErr: c.closeErr, onClose: c.onClose}, nil
	}
	if c.respond != nil {
		columns, values := c.respond(s)
		return &testDriverRows{columns: columns, values: values, closeErr: c.closeErr, onClose: c.onClose}, nil
	}
	return &testDriverRows{columns: c.columns, values: c.values, closeErr: c.closeErr, onClose: c.onClose}, nil
}

type testTransaction struct{ err error }

func (tx testTransaction) Commit() error   { return tx.err }
func (tx testTransaction) Rollback() error { return nil }

type testDriverRows struct {
	columns  []string
	values   [][]driver.Value
	index    int
	onClose  func()
	closeErr error
}

func (r *testDriverRows) Columns() []string { return r.columns }
func (r *testDriverRows) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return r.closeErr
}
func (r *testDriverRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func testDB(t *testing.T, c testConnector, opts ...database.Option) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: c}, database.DefaultPoolConfig(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}

type hookedInput struct{ Text string }
type hookedStored string
type hookedRow struct {
	ID        int64
	Name      string `foundry:"default=database"`
	Note      value.Nullable[hookedStored]
	Body      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt value.Nullable[time.Time]
	Scratch   string `foundry:"-"`
}
type hookedTable struct {
	orm.Table[hookedRow, int64]
	Name    orm.InputText[hookedRow, string, string]
	Note    orm.NullableInputText[hookedRow, hookedInput, hookedStored]
	Body    orm.Column[hookedRow, []byte]
	Created orm.CreatedAt[hookedRow, time.Time]
	Updated orm.UpdatedAt[hookedRow, time.Time]
	Deleted orm.SoftDelete[hookedRow, time.Time]
}

func hookedFields(factory func() orm.Hooks[hookedRow], extra ...orm.Option) hookedTable {
	opts := append([]orm.Option{}, extra...)
	if factory != nil {
		opts = append(opts, orm.WithHooks(factory))
	}
	return orm.Define("hooked", func(b orm.Builder[hookedRow], m *hookedRow) hookedTable {
		return hookedTable{orm.TableOf(b, &m.ID), orm.InputTextOf(b, &m.Name, func(s string) (string, error) { return strings.TrimSpace(s), nil }), orm.NullableInputTextOf(b, &m.Note, func(v hookedInput) (hookedStored, error) { return hookedStored(strings.TrimSpace(v.Text)), nil }), orm.BytesOf(b, &m.Body), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt), orm.SoftDeleteOf(b, &m.DeletedAt)}
	}, opts...)
}

func hookDB(t *testing.T, c testConnector, declarations ...lifecycle.Declaration) *database.DB {
	t.Helper()
	db, err := database.Prepare(database.Adapter{Connector: c}, database.DefaultPoolConfig(), database.WithClock(fixedNow{time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	set, err := lifecycle.NewObservers(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BindObservers(set); err != nil {
		t.Fatal(err)
	}
	if err = db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

var hookColumns = []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}

func hookValues(name string, note driver.Value, deleted driver.Value) [][]driver.Value {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	return [][]driver.Value{{int64(1), name, note, []byte{1, 2}, now, now, deleted}}
}

type relationPerson struct {
	ID        int64
	ManagerID value.Nullable[int64]
	Name      string
	DeletedAt value.Nullable[temporal.DateTime]
	Manager   relation.One[relationPerson]
	Reports   relation.Many[relationPerson]
}
type peopleTable struct {
	orm.Table[relationPerson, int64]
	ID        orm.Key[relationPerson, int64]
	ManagerID orm.NullableNumber[relationPerson, int64]
	Name      orm.Text[relationPerson, string]
	DeletedAt orm.SoftDelete[relationPerson, temporal.DateTime]
	Manager   orm.One[relationPerson, relationPerson]
	Reports   orm.Many[relationPerson, relationPerson]
}

func people() peopleTable {
	return orm.Define("people", func(b orm.Builder[relationPerson], p *relationPerson) peopleTable {
		tbl := orm.TableOf(b, &p.ID)
		return peopleTable{Table: tbl, ID: tbl.Key(), ManagerID: orm.NullableNumberOf(b, &p.ManagerID), Name: orm.TextOf(b, &p.Name), DeletedAt: orm.SoftDeleteOf(b, &p.DeletedAt), Manager: orm.OneOf(b, &p.Manager), Reports: orm.ManyOf(b, &p.Reports)}
	})
}

var personColumns = []string{"id", "manager_id", "name", "deleted_at"}

type relationOracle struct {
	rows          [][]driver.Value
	statements    []string
	calls, closed int
	retain        bool
}

func relationRow(id int64, manager driver.Value, name string) []driver.Value {
	return []driver.Value{id, manager, name, nil}
}
func treeOracle() *relationOracle {
	return &relationOracle{retain: true, rows: [][]driver.Value{
		relationRow(1, nil, "root"), relationRow(2, nil, "root"),
		relationRow(10, int64(1), "child a"), relationRow(11, int64(1), "child b"), relationRow(12, int64(2), "child c"),
		relationRow(20, int64(10), "grand a"), relationRow(21, int64(12), "grand c"), relationRow(30, int64(20), "great"),
	}}
}
func (o *relationOracle) connector() testConnector {
	return testConnector{onClose: func() { o.closed++ }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		if o.closed != o.calls {
			return nil, nil, errors.New("next statement began before prior rows closed")
		}
		o.calls++
		if o.retain {
			o.statements = append(o.statements, sql)
		}
		keys := map[int64]bool{}
		member := strings.Contains(sql, "ANY(")
		for _, a := range args {
			if s, ok := a.Value.(string); ok && strings.HasPrefix(s, "{") {
				for _, n := range strings.Split(strings.Trim(s, "{}"), ",") {
					id, err := strconv.ParseInt(n, 10, 64)
					if err != nil {
						return nil, nil, err
					}
					keys[id] = true
				}
			}
		}
		var result [][]driver.Value
		for _, row := range o.rows {
			index := 0
			if strings.Contains(sql, `"manager_id" = ANY(`) {
				index = 1
			}
			if member {
				id, ok := row[index].(int64)
				if !ok || !keys[id] {
					continue
				}
			}
			if strings.Contains(sql, `"name" = $`) {
				if len(args) > 0 && row[2] != args[0].Value {
					continue
				}
			}
			if strings.Contains(sql, `"name" LIKE $`) && !strings.HasPrefix(row[2].(string), strings.TrimSuffix(args[0].Value.(string), "%")) {
				continue
			}
			result = append(result, row)
		}
		sort.Slice(result, func(i, j int) bool {
			if strings.Contains(sql, `"name" DESC`) && result[i][2] != result[j][2] {
				return result[i][2].(string) > result[j][2].(string)
			}
			return result[i][0].(int64) < result[j][0].(int64)
		})
		if strings.Contains(sql, " LIMIT ") {
			if limit, ok := args[len(args)-1].Value.(int64); ok && len(result) > int(limit) {
				result = result[:limit]
			}
		}
		return personColumns, result, nil
	}}
}

type aggregateModel struct {
	ID       int64
	ParentID int64
	Children relation.Many[aggregateModel]
	Count    relation.Value[int64]
	Links    relation.Through[aggregateModel, aggregateModel]
}
type aggregateTable struct {
	orm.Table[aggregateModel, int64]
	ParentID orm.Number[aggregateModel, int64]
	Children orm.Many[aggregateModel, aggregateModel]
	Count    orm.Value[aggregateModel, int64]
	Links    orm.Through[aggregateModel, aggregateModel, aggregateModel]
}

func aggregateModels() aggregateTable {
	return orm.Define("aggregate_models", func(b orm.Builder[aggregateModel], m *aggregateModel) aggregateTable {
		return aggregateTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.ManyOf(b, &m.Children), orm.ValueOf(b, &m.Count), orm.ThroughOf(b, &m.Links)}
	})
}

type catcher[K any] struct {
	*database.DB
	calls, transactions int
	captured            K
}

func catchObservers[K any](db *database.DB, _ func(K) (lifecycle.Observers, bool)) *catcher[K] {
	return &catcher[K]{DB: db}
}

func (c *catcher[K]) FoundryObservers(k K) (lifecycle.Observers, bool) {
	c.calls++
	c.captured = k
	return lifecycle.Observers{}, true
}

func (c *catcher[K]) Transaction(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	c.transactions++
	return c.DB.Transaction(ctx, fn, options...)
}

type externalValue struct {
	Loaded bool
	Value  string
}
type extensionModel struct {
	ID       int64
	External externalValue
}
type extensionTable struct {
	orm.Table[extensionModel, int64]
	External orm.Slot[extensionModel, externalValue]
}

func extensionFields() extensionTable {
	return orm.Define("adapter_owners", func(b orm.Builder[extensionModel], m *extensionModel) extensionTable {
		return extensionTable{orm.TableOf(b, &m.ID), orm.SlotOf(b, &m.External)}
	})
}

type scopeTransferRow struct {
	ID, Rank int64
	Children relation.Many[scopeTransferRow]
	Links    relation.Through[scopeTransferRow, scopeTransferRow]
}
type scopeTransferTable struct {
	orm.Table[scopeTransferRow, int64]
	Rank     orm.Number[scopeTransferRow, int64]
	Children orm.Many[scopeTransferRow, scopeTransferRow]
	Links    orm.Through[scopeTransferRow, scopeTransferRow, scopeTransferRow]
}

func scopeTransferFields(options ...orm.Option) scopeTransferTable {
	return orm.Define("scope_records", func(b orm.Builder[scopeTransferRow], m *scopeTransferRow) scopeTransferTable {
		return scopeTransferTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Rank), orm.ManyOf(b, &m.Children), orm.ThroughOf(b, &m.Links)}
	}, options...)
}

type secureRecord struct {
	ID     model.ID[secureRecord]
	Secret encrypted.Text
	Backup value.Nullable[encrypted.Text]
}
type secureTable struct {
	orm.Table[secureRecord, model.ID[secureRecord]]
	Secret orm.Encrypted[secureRecord, encrypted.Text]
	Backup orm.NullableEncrypted[secureRecord, encrypted.Text]
}

func secureRecords() secureTable { return secureRecordsAt("secure_records") }
func secureRecordsAt(name string) secureTable {
	return orm.Define(name, func(b orm.Builder[secureRecord], m *secureRecord) secureTable {
		return secureTable{orm.TableOf(b, &m.ID), orm.EncryptedTextOf(b, &m.Secret), orm.NullableEncryptedTextOf(b, &m.Backup)}
	})
}

func sensitiveKeys(t *testing.T) *encryption.Keyring {
	t.Helper()
	key, err := encryption.GenerateKey("field_test")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring("field_test", key)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}
