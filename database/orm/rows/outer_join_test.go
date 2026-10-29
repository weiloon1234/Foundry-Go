package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type outerRecord struct {
	ID       value.Nullable[int64]
	Nickname value.Nullable[string]
}
type outerFields struct {
	rows.Row[outerRecord]
	ID       rows.Field[outerRecord, value.Nullable[int64]]
	Nickname rows.Field[outerRecord, value.Nullable[string]]
}

func outerSchema() outerFields {
	return rows.Define(func(b rows.Builder[outerRecord], r *outerRecord) outerFields {
		return outerFields{b.Row(), rows.NullableNumberOf(b, &r.ID), rows.NullableTextOf(b, &r.Nickname)}
	})
}
func TestOuterJoinNullabilityAndKinds(t *testing.T) {
	m := declareMembers()
	a, b := rows.As[left](m.Query(), "a"), rows.As[right](m.Query(), "b")
	on := rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.KeyExpr(m.ID)))
	l, r, f := rows.LeftJoin(a, b, on), rows.RightJoin(a, b, on), rows.FullJoin(a, b, on)
	s := outerSchema()
	project := func(source rows.Source[rows.Left[rows.Alias[left, member], rows.Alias[right, member]]]) rows.Query[rows.Left[rows.Alias[left, member], rows.Alias[right, member]], outerRecord] {
		scope := rows.NullableRightScope(l, rows.Scope(b))
		return rows.Select(source, s, rows.Map(s.ID, expr.AtNullable(scope, orm.KeyExpr(m.ID))), rows.Map(s.Nickname, expr.AtNullableValue(scope, orm.NullableTextExpr(m.Nickname))))
	}
	state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}, {int64(3), "nick"}}}
	got, err := rows.Read(project(l)).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].ID.IsNull() || !got[0].Nickname.IsNull() || got[1].ID != value.Of(int64(3)) {
		t.Fatal(got, err)
	}
	if !strings.Contains(state.sql, `LEFT JOIN "members" AS "b"`) {
		t.Fatal(state.sql)
	}
	queries := []struct {
		kind string
		run  func(*driverState) error
	}{
		{"RIGHT", func(state *driverState) error {
			scope := rows.NullableLeftScope(r, rows.Scope(a))
			_, err := rows.Read(rows.Select(r, s, rows.Map(s.ID, expr.AtNullable(scope, orm.KeyExpr(m.ID))), rows.Map(s.Nickname, expr.AtNullableValue(scope, orm.NullableTextExpr(m.Nickname))))).All(t.Context(), openTest(t, state))
			return err
		}},
		{"FULL", func(state *driverState) error {
			x, y := rows.NullableLeftScope(f, rows.Scope(a)), rows.NullableRightScope(f, rows.Scope(b))
			_, err := rows.Read(rows.Select(f, s, rows.Map(s.ID, expr.AtNullable(x, orm.KeyExpr(m.ID))), rows.Map(s.Nickname, expr.AtNullableValue(y, orm.NullableTextExpr(m.Nickname))))).All(t.Context(), openTest(t, state))
			return err
		}},
	}
	for _, q := range queries {
		t.Run(q.kind, func(t *testing.T) {
			state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}}}
			if err := q.run(state); err != nil || !strings.Contains(state.sql, q.kind+` JOIN "members"`) {
				t.Fatal(state.sql, err)
			}
		})
	}
}
func TestNullableScopeChainAndSourceBoundaries(t *testing.T) {
	m := declareMembers()
	o := declareOrders()
	a, b := rows.As[left](m.Query().Where(orm.TextEq(m.Name, "Ada")).Limit(2), "a"), rows.As[right](o.Query().Where(orm.NumberGt(o.MemberID, 0)), "b")
	joined := rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.NumberExpr(o.MemberID))))
	type third struct{}
	c := rows.As[third](m.Query(), "c")
	chain := rows.CrossJoin(joined, c)
	scope := rows.LeftNullableScope(chain, rows.NullableRightScope(joined, rows.Scope(b)))
	type one struct{ ID value.Nullable[int64] }
	type fields struct {
		rows.Row[one]
		ID rows.Field[one, value.Nullable[int64]]
	}
	schema := rows.Define(func(b rows.Builder[one], r *one) fields { return fields{b.Row(), rows.NullableNumberOf(b, &r.ID)} })
	e := expr.AtNullable(scope, orm.KeyExpr(o.ID))
	core := expr.FoundryExpression(e, sqlowner.Expressions())
	if err := ormcore.CheckReferences(rows.SourceOf(chain), core.Refs); err != nil {
		t.Fatal(err)
	}
	state := &driverState{columns: []string{"id"}, rows: [][]driver.Value{{nil}}}
	q := rows.Select(chain, schema, rows.Map(schema.ID, e)).Where(e.IsNull())
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.sql, `CROSS JOIN`) || !strings.Contains(state.sql, `LIMIT $2`) || !strings.Contains(state.sql, `"orders" WHERE`) {
		t.Fatal(state.sql)
	}
	state = &driverState{}
	other := declareOrders()
	bad := rows.Select(chain, schema, rows.Map(schema.ID, expr.AtNullable(scope, orm.KeyExpr(other.ID))))
	if _, err := rows.Read(bad).All(t.Context(), openTest(t, state)); err == nil || state.sql != "" {
		t.Fatal("foreign declaration accepted", err)
	}
}
func TestJoinRuntimeOwnershipFailures(t *testing.T) {
	m := declareMembers()
	a := rows.As[left](m.Query(), "a")
	b := rows.As[right](m.Query(), "b")
	on := rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.KeyExpr(m.ID)))
	for _, j := range []rows.LeftJoined[rows.Alias[left, member], rows.Alias[right, member]]{
		rows.LeftJoin(a, rows.As[right](m.Query(), "a"), on),
		rows.LeftJoin(a, b, rows.JoinOn[rows.Alias[left, member], rows.Alias[right, member]]{}),
		rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(rows.As[left](m.Query(), "other")), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.KeyExpr(m.ID)))),
	} {
		if rows.SourceOf(j).Err == nil {
			t.Fatal("invalid join accepted")
		}
	}
	same := rows.CrossJoin(a, rows.As[left](m.Query(), "other"))
	if rows.SourceOf(same).Err == nil {
		t.Fatal("repeated typed alias accepted")
	}
	j := rows.LeftJoin(a, b, on)
	wrong := rows.NullableRightScope(j, rows.Scope(rows.As[right](m.Query(), "other")))
	if expr.FoundryNullableScopeData(wrong, sqlowner.Expressions()).Err == nil {
		t.Fatal("same typed foreign alias accepted")
	}
	var missing *rows.LeftJoined[rows.Alias[left, member], rows.Alias[right, member]]
	if expr.FoundryScopeData(rows.LeftScope(missing, rows.Scope(a)), sqlowner.Expressions()).Err == nil {
		t.Fatal("nil join accepted")
	}
}
