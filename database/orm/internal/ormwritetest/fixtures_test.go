package ormwritetest

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
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

type timed struct {
	ID        int
	CreatedAt temporal.DateTime
	UpdatedAt temporal.DateTime
	DeletedAt value.Nullable[temporal.DateTime]
}
type timedTable struct {
	orm.Table[timed, int]
	CreatedAt orm.CreatedAt[timed, temporal.DateTime]
	UpdatedAt orm.UpdatedAt[timed, temporal.DateTime]
	DeletedAt orm.SoftDelete[timed, temporal.DateTime]
}

func timedRecords(opts ...orm.Option) timedTable {
	return orm.Define("timed", func(b orm.Builder[timed], m *timed) timedTable {
		return timedTable{orm.TableOf(b, &m.ID), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt), orm.SoftDeleteOf(b, &m.DeletedAt)}
	}, opts...)
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

type eachRow struct {
	ID   int64
	Name string
}
type eachTable struct {
	orm.Table[eachRow, int64]
	Name orm.Text[eachRow, string]
}

func eachFields(h orm.Hooks[eachRow]) eachTable {
	return orm.Define("each_records", func(b orm.Builder[eachRow], r *eachRow) eachTable {
		return eachTable{orm.TableOf(b, &r.ID), orm.TextOf(b, &r.Name)}
	}, orm.WithHooks(func() orm.Hooks[eachRow] { return h }))
}

type noEachWriter struct{ calls int }

func (w *noEachWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	w.calls++
	return errors.New("unexpected transaction")
}

type privateLifecycleError struct{ modelValue string }

func (e *privateLifecycleError) Error() string { return "rejected model value: " + e.modelValue }
func requireSafeLifecycleError(t *testing.T, err error, cause *privateLifecycleError) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatal("error identity lost")
	}
	var got *privateLifecycleError
	if !errors.As(err, &got) || got != cause {
		t.Fatal("typed cause lost")
	}
	for _, formatted := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(formatted, cause.modelValue) {
			t.Fatal("model value leaked in error formatting")
		}
	}
}

type transformRow struct {
	ID    int64
	First string
	Maybe value.Nullable[string]
	Last  string
}
type transformTable struct {
	orm.Table[transformRow, int64]
	First orm.InputText[transformRow, string, string]
	Maybe orm.NullableInputText[transformRow, string, string]
	Last  orm.InputText[transformRow, string, string]
}

func transformFields(first, last func(string) (string, error), maybe func(string) (string, error)) transformTable {
	return orm.Define("transforms", func(b orm.Builder[transformRow], m *transformRow) transformTable {
		return transformTable{orm.TableOf(b, &m.ID), orm.InputTextOf(b, &m.First, first), orm.NullableInputTextOf(b, &m.Maybe, maybe), orm.InputTextOf(b, &m.Last, last)}
	})
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
