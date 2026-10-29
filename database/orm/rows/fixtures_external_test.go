package rows_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct {
	ID       int64
	Name     string
	Nickname value.Nullable[string]
}
type members struct {
	orm.Table[member, int64]
	ID       orm.Primary[member, int64]
	Name     orm.Text[member, string]
	Nickname orm.NullableText[member, string]
}

func declareMembers() members {
	return orm.Define("members", func(b orm.Builder[member], m *member) members {
		tbl := orm.TableOf(b, &m.ID)
		return members{tbl, orm.PrimaryOf(b, tbl.Key()), orm.TextOf(b, &m.Name), orm.NullableTextOf(b, &m.Nickname)}
	})
}

type order struct {
	ID       int64
	MemberID int64
}
type orders struct {
	orm.Table[order, int64]
	ID       orm.Key[order, int64]
	MemberID orm.Number[order, int64]
}

func declareOrders() orders {
	return orm.Define("orders", func(b orm.Builder[order], o *order) orders {
		tbl := orm.TableOf(b, &o.ID)
		return orders{tbl, tbl.Key(), orm.NumberOf(b, &o.MemberID)}
	})
}

type left struct{}
type right struct{}
type record struct {
	ID   int64
	Name string
}
type fields struct {
	rows.Row[record]
	ID   rows.Field[record, int64]
	Name rows.Field[record, string]
}

func declareRows() fields {
	return rows.Define(func(b rows.Builder[record], r *record) fields {
		return fields{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Name)}
	})
}

type driverState struct {
	arguments []driver.NamedValue
	sql       string
	columns   []string
	rows      [][]driver.Value
	closed    bool
	closeErr  error
}
type testDriver struct{ state *driverState }

func (d testDriver) Driver() driver.Driver                        { return d }
func (d testDriver) Open(string) (driver.Conn, error)             { return &testConn{d.state}, nil }
func (d testDriver) Connect(context.Context) (driver.Conn, error) { return d.Open("") }

type testConn struct{ state *driverState }

func (c *testConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (c *testConn) Close() error                        { return nil }
func (c *testConn) Begin() (driver.Tx, error)           { return nil, errors.New("unexpected transaction") }
func (c *testConn) Ping(context.Context) error          { return nil }
func (c *testConn) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.sql = sql
	c.state.arguments = append([]driver.NamedValue(nil), args...)
	return &testRows{state: c.state}, nil
}

type testRows struct {
	state *driverState
	index int
}

func (r *testRows) Columns() []string { return r.state.columns }
func (r *testRows) Close() error      { r.state.closed = true; return r.state.closeErr }
func (r *testRows) Next(dest []driver.Value) error {
	if r.index >= len(r.state.rows) {
		return io.EOF
	}
	copy(dest, r.state.rows[r.index])
	r.index++
	return nil
}

func openTest(t *testing.T, state *driverState) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: testDriver{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	return db
}

type recursionRecord struct {
	ID   int64
	Rank value.Nullable[int64]
}
type recursionFields struct {
	orm.Table[recursionRecord, int64]
	Rank orm.NullableNumber[recursionRecord, int64]
}

var recursionTable = orm.Define("records", func(b orm.Builder[recursionRecord], m *recursionRecord) recursionFields {
	return recursionFields{orm.TableOf(b, &m.ID), orm.NullableNumberOf(b, &m.Rank)}
})

func rejectScalar[S, V any](t *testing.T, source rows.SelectionSource[S], v expr.Selected[S, V], c codec.Codec[V]) {
	t.Helper()
	state := &driverState{}
	_, err := rows.Read(expressionQuery(source, v, c)).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatalf("invalid scalar reached executor: %v, %s", err, state.sql)
	}
}
