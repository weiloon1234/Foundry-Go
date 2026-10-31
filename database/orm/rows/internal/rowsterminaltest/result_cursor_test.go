package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestResultCursorWindowAndBidirectionalNavigation(t *testing.T) {
	m := declareMembers()
	c := rows.CursorFor(rows.ModelRecord(m.Query().Where(orm.KeyNe(m.ID, 0)).OrderBy(orm.KeyDesc(m.ID)).Limit(8).Offset(1)))
	id := expr.At(c.Scope(), orm.KeyExpr(m.ID))
	c = c.UniqueBy(expr.Group(id))
	state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "one", nil}, {int64(2), "two", nil}}}
	db := openTest(t, state)
	page, err := c.Paginate(t.Context(), db, orm.CursorRequest[member]{Size: 1})
	if err != nil || len(page.Items) != 1 || !page.Next.IsSet() || page.Previous.IsSet() || !state.closed {
		t.Fatal(page, err, state.closed)
	}
	if !strings.Contains(state.sql, `ORDER BY "members"."id" DESC LIMIT $2 OFFSET $3) AS "foundry_cursor"`) || !strings.Contains(state.sql, `ORDER BY "foundry_cursor"."id" ASC LIMIT $4`) {
		t.Fatal(state.sql)
	}
	state.rows = [][]driver.Value{{int64(2), "two", nil}}
	state.closed = false
	next, err := c.Paginate(t.Context(), db, orm.CursorRequest[member]{Size: 1, After: page.Next})
	if err != nil || !next.Previous.IsSet() || next.Next.IsSet() || !strings.Contains(state.sql, `"foundry_cursor"."id" >`) {
		t.Fatal(next, err, state.sql)
	}
	state.rows = [][]driver.Value{{int64(1), "one", nil}}
	previous, err := c.Paginate(t.Context(), db, orm.CursorRequest[member]{Size: 1, Before: next.Previous})
	if err != nil || len(previous.Items) != 1 || !strings.Contains(state.sql, `ORDER BY "foundry_cursor"."id" DESC`) {
		t.Fatal(previous, err, state.sql)
	}
	state.sql = ""
	changed := c.Where(expr.Ne(id, 99))
	if _, err := changed.Paginate(t.Context(), db, orm.CursorRequest[member]{Size: 1, After: page.Next}); !errors.As(err, new(*orm.CursorInputError)) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestResultCursorNullableValueAndInvalidDeclarations(t *testing.T) {
	m := declareMembers()
	c := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname)))))))
	c = c.UniqueBy(expr.Group(c.Value()))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"nick"}}}
	db := openTest(t, state)
	page, err := c.Paginate(t.Context(), db, orm.CursorRequest[value.Nullable[string]]{Size: 1})
	if err != nil || !page.Items[0].IsNull() || !page.Next.IsSet() {
		t.Fatal(page, err)
	}
	state.rows = nil
	if _, err := c.Paginate(t.Context(), db, orm.CursorRequest[value.Nullable[string]]{Size: 1, After: page.Next}); err != nil {
		t.Fatal(err)
	}
	state.sql = ""
	if _, err := c.Paginate(t.Context(), db, orm.CursorRequest[value.Nullable[string]]{Size: 1, After: value.Set(orm.Cursor[value.Nullable[string]]{})}); err == nil || state.sql != "" {
		t.Fatal(err, state.sql)
	}
	base := rows.CursorFor(rows.ModelRecord(m.Query()))
	id := expr.At(base.Scope(), orm.KeyExpr(m.ID))
	foreign := rows.CursorFor(rows.ModelRecord(declareMembers().Query()))
	cases := []rows.CursorQuery[member]{base, base.UniqueBy(expr.Group(id), expr.Group(id)), base.UniqueBy(expr.Group(id)).OrderBy(expr.Asc(id), expr.Desc(id)), base.UniqueBy(expr.Group(expr.At(foreign.Scope(), orm.KeyExpr(m.ID)))), base.UniqueBy(expr.Group(id)).OrderBy(expr.Asc(id).NullsLast()), {}, base.UniqueBy(expr.Group(id)).OrderBy(expr.AscValue(expr.AddValue(expr.AsSelected(expr.Row(id)), expr.AsSelected(expr.Row(id)))))}
	for i, bad := range cases {
		if _, err := bad.Paginate(t.Context(), db, orm.CursorRequest[member]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
			t.Fatal(i, err, state.sql)
		}
	}
}

func TestResultCursorFailureDiscardsPageAndRejectsSensitiveKeys(t *testing.T) {
	m := declareMembers()
	c := rows.CursorFor(rows.ModelRecord(m.Query()))
	c = c.UniqueBy(expr.Group(expr.At(c.Scope(), orm.KeyExpr(m.ID))))
	for _, state := range []*driverState{{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "one", nil}}, closeErr: errors.New("close failure")}, {columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "one", nil}, {"bad", "two", nil}}}} {
		page, err := c.Paginate(t.Context(), openTest(t, state), orm.CursorRequest[member]{Size: 1})
		if err == nil || page.Items != nil || page.Next.IsSet() || !state.closed {
			t.Fatal(page, err, state.closed)
		}
	}
	type secret struct {
		ID   int64
		Name string
	}
	type secretFields struct {
		orm.Table[secret, int64]
		Name orm.Column[secret, string]
	}
	table := orm.Define("secrets", func(b orm.Builder[secret], s *secret) secretFields {
		return secretFields{orm.TableOf(b, &s.ID), orm.ColumnOf(b, &s.Name, orm.Codec(codec.String[string]().WithSensitiveValues()))}
	})
	scalar := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(table.Query()), expr.AsSelected(expr.Row(expr.Value(orm.ColumnExpr(table.Name)))))))
	state := &driverState{}
	if _, err := scalar.UniqueBy(expr.Group(scalar.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[string]{Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestResultCursorProjectionFieldCodecs(t *testing.T) {
	m := declareMembers()
	schema := declareRows()
	report := rows.Select(rows.From(m.Query()), schema, rows.Map(schema.ID, expr.Value(orm.KeyExpr(m.ID))), rows.Map(schema.Name, expr.Value(orm.TextExpr(m.Name))))
	c := rows.CursorFor(rows.RecordOf(report))
	c = c.UniqueBy(expr.Group(rows.At(c.Scope(), schema.ID)))
	state := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(1), "one"}, {int64(2), "two"}}}
	db := openTest(t, state)
	p, err := c.Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1})
	if err != nil || !p.Next.IsSet() {
		t.Fatal(p, err)
	}
	state.rows = [][]driver.Value{{int64(2), "two"}}
	p, err = c.Paginate(t.Context(), db, orm.CursorRequest[record]{Size: 1, After: p.Next})
	if err != nil || !p.Previous.IsSet() || p.Items[0].ID != 2 {
		t.Fatal(p, err)
	}
}
