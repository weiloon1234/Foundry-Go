package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
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
	Row[record]
	ID   Field[record, int64]
	Name Field[record, string]
}

func declareRows() fields {
	return Define(func(b Builder[record], r *record) fields {
		return fields{b.Row(), NumberOf(b, &r.ID), TextOf(b, &r.Name)}
	})
}
func projection() Query[Inner[Alias[left, member], Alias[right, order]], record] {
	m, o := declareMembers(), declareOrders()
	a, b := As[left](m.Query(), "m"), As[right](o.Query(), "o")
	j := InnerJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), o.MemberID)))
	schema := declareRows()
	id := expr.At(RightScope(j, Scope(b)), o.ID)
	name := expr.At(LeftScope(j, Scope(a)), m.Name)
	return Select(j, schema, Map(schema.Name, name), Map(schema.ID, id)).Where(name.Eq("Ada")).OrderBy(id.Asc())
}
func TestJoinProjectionAllUsesDeclaredFieldOrder(t *testing.T) {
	state := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(9), "Ada"}}}
	db := openTest(t, state)
	got, err := projection().All(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (record{9, "Ada"}) || !state.closed {
		t.Fatal(got, state.closed)
	}
	want := `SELECT "o"."id" AS "id", "m"."name" AS "name" FROM "members" AS "m" INNER JOIN "orders" AS "o" ON ("m"."id" = "o"."member_id") WHERE ("m"."name" = $1) ORDER BY "o"."id" ASC`
	if state.sql != want {
		t.Fatalf("SQL\n%s\nwant\n%s", state.sql, want)
	}
}
func TestProjectionDiscardsPartialRowsAndCloseFailure(t *testing.T) {
	for _, state := range []*driverState{
		{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(9), "Ada"}, {"bad integer", "Bad"}}},
		{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(9), "Ada"}}, closeErr: errors.New("close failure")},
	} {
		got, err := projection().All(t.Context(), openTest(t, state))
		if got != nil || err == nil || !state.closed {
			t.Fatal(got, err, state.closed)
		}
	}
}
func TestSelectValuePreservesNullableCodec(t *testing.T) {
	m := declareMembers()
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"nick"}}}
	got, err := SelectValue(m.Query(), m.Nickname).All(t.Context(), openTest(t, state))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].IsNull() {
		t.Fatal(got)
	}
	v, ok := got[1].Get()
	if !ok || v != "nick" {
		t.Fatal(got)
	}
}
func TestRowStartupAndMappingErrors(t *testing.T) {
	bad := Define(func(b Builder[record], r *record) fields {
		return fields{Row: b.Row(), ID: NumberOf(b, &r.ID)}
	})
	if err := Check(bad); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatal(err)
	}
	duplicate := Define(func(b Builder[record], r *record) fields {
		NumberOf(b, &r.ID)
		return fields{b.Row(), NumberOf(b, &r.ID), TextOf(b, &r.Name)}
	})
	if Check(duplicate) == nil {
		t.Fatal("duplicate binding accepted")
	}
	missing := Define(func(b Builder[record], r *record) fields {
		return fields{ID: NumberOf(b, &r.ID), Name: TextOf(b, &r.Name)}
	})
	if Check(missing) == nil {
		t.Fatal("unassigned row accepted")
	}
	invalidCodec := Define(func(b Builder[record], r *record) fields {
		return fields{b.Row(), FieldOf(b, &r.ID, orm.Codec(codec.Codec[int64]{})), TextOf(b, &r.Name)}
	})
	if Check(invalidCodec) == nil {
		t.Fatal("invalid codec accepted")
	}
	m, other := declareMembers(), declareMembers()
	a := As[left](m.Query(), "m")
	s, foreign := declareRows(), declareRows()
	state := &driverState{}
	db := openTest(t, state)
	for _, q := range []Query[Alias[left, member], record]{
		Select(a, s, Map(s.ID, expr.At(Scope(a), m.ID))),
		Select(a, s, Map(s.ID, expr.At(Scope(a), m.ID)), Map(foreign.Name, expr.At(Scope(a), m.Name))),
		Select(a, s, Map(s.ID, expr.At(Scope(a), other.ID)), Map(s.Name, expr.At(Scope(a), m.Name))),
	} {
		if _, err := q.All(t.Context(), db); err == nil {
			t.Fatal("invalid mapping accepted")
		}
	}
	if state.sql != "" {
		t.Fatal("invalid projection executed SQL")
	}
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

func TestNilEmbeddedRowAndStartupFaults(t *testing.T) {
	type pointerFields struct{ *Row[record] }
	schema := Define(func(b Builder[record], r *record) pointerFields {
		NumberOf(b, &r.ID)
		TextOf(b, &r.Name)
		return pointerFields{}
	})
	if err := Check(schema); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil embedded row: %v", err)
	}
	missing := Define(func(b Builder[record], r *record) Row[record] { NumberOf(b, &r.ID); return b.Row() })
	if err := Check(missing); !errors.Is(err, fault.Invalid) {
		t.Fatalf("undeclared field: %v", err)
	}
}

func TestNullableJoinComparisonAndProjection(t *testing.T) {
	type person struct {
		ID     int64
		Parent value.Nullable[int64]
	}
	type table struct {
		orm.Table[person, int64]
		Parent orm.NullableColumn[person, int64]
	}
	people := orm.Define("people", func(b orm.Builder[person], p *person) table {
		return table{orm.TableOf(b, &p.ID), orm.NullableColumnOf(b, &p.Parent)}
	})
	a, b := As[left](people.Query(), "child"), As[right](people.Query(), "parent")
	joined := InnerJoin(a, b, On(expr.At(Scope(a), people.Parent), expr.At(Scope(b), people.Key())))
	type result struct{ Parent value.Nullable[int64] }
	type fields struct {
		Row[result]
		Parent Field[result, value.Nullable[int64]]
	}
	schema := Define(func(b Builder[result], r *result) fields { return fields{b.Row(), NullableNumberOf(b, &r.Parent)} })
	parent := expr.At(LeftScope(joined, Scope(a)), people.Parent)
	q := Select(joined, schema, Map(schema.Parent, parent)).Where(parent.Eq(3), parent.Ne(4), parent.IsNotNull())
	state := &driverState{columns: []string{"parent"}, rows: [][]driver.Value{{int64(3)}}}
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if n, ok := got[0].Parent.Get(); !ok || n != 3 {
		t.Fatal(got)
	}
	want := `SELECT "child"."parent" AS "parent" FROM "people" AS "child" INNER JOIN "people" AS "parent" ON ("child"."parent" = "parent"."id") WHERE ("child"."parent" = $1) AND ("child"."parent" <> $2) AND ("child"."parent" IS NOT NULL)`
	if state.sql != want {
		t.Fatalf("SQL: %s", state.sql)
	}
}

func TestForeignEmbeddedRowPointerDoesNotMutateItsOwner(t *testing.T) {
	original := declareRows()
	type pointerFields struct{ *Row[record] }
	foreign := Define(func(b Builder[record], r *record) pointerFields {
		NumberOf(b, &r.ID)
		TextOf(b, &r.Name)
		return pointerFields{&original.Row}
	})
	if err := Check(foreign); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := Check(original); err != nil {
		t.Fatalf("foreign declaration corrupted original: %v", err)
	}
}
