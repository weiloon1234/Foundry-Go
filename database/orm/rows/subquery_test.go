package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSubqueryHydrationAndPhaseBoundary(t *testing.T) {
	m := declareMembers()
	o := declareOrders()
	outer := rows.From(m.Query())
	inner := rows.SelectScalar(rows.From(o.Query().Where(orm.NumberGt(o.MemberID, 4)).Limit(1)), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(o.ID)))))
	nullable := rows.SelectScalar(rows.From(m.Query().Where(orm.KeyEq(m.ID, 1))), expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname)))))
	s := outerSchema()
	q := rows.Select(outer, s, rows.MapSelected(s.ID, expr.AsSelected(rows.ScalarRowQuery(outer, inner))), rows.MapSelected(s.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(outer, nullable)))).Where(rows.ExistsQuery(outer, inner), rows.InQuery(expr.Value(orm.KeyExpr(m.ID)), inner), expr.Not(rows.InNullableQuery(expr.Value(orm.TextExpr(m.Name)), nullable)))
	state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}, {int64(9), "nick"}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].ID.IsNull() || !got[0].Nickname.IsNull() || got[1].ID != value.Of(int64(9)) {
		t.Fatal(got, err)
	}
	if strings.Count(state.sql, "SELECT") != 6 || !strings.Contains(state.sql, "EXISTS(") || !strings.Contains(state.sql, " IN (") || !strings.Contains(state.sql, "NOT ") {
		t.Fatal(state.sql)
	}
	// Inner aggregate ownership does not turn its scalar result into an outer aggregate.
	count := rows.SelectScalar(rows.From(o.Query()), expr.AsSelected(expr.Count[order]()))
	row := rows.ScalarRowQuery(outer, count)
	core := expr.FoundryRowValue(row, sqlowner.Expressions())
	if _, ok := core.Node.(querycore.ScalarSubquery); !ok {
		t.Fatalf("lost SELECT boundary: %T", core.Node)
	}
	if err := ormcore.CheckReferences(rows.SourceOf(outer), core.Refs); err != nil {
		t.Fatal(err)
	}
}
func TestSubqueriesRetainErrorsAndOuterDeclarations(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	outer := rows.From(m.Query())
	foreign := rows.From(other.Query())
	valid := rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID)))))
	bad := rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(other.ID)))))
	nullable := rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Nickname)))))
	s := outerSchema()
	for _, q := range []rows.Query[member, outerRecord]{
		rows.Select(outer, s, rows.MapSelected(s.ID, expr.AsSelected(rows.ScalarRowQuery(outer, bad))), rows.MapSelected(s.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(outer, nullable)))),
		rows.Select(outer, s, rows.MapSelected(s.ID, expr.AsSelected(rows.ScalarRowQuery(foreign, valid))), rows.MapSelected(s.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(outer, nullable)))),
		rows.Select(outer, s, rows.MapSelected(s.ID, expr.AsSelected(rows.ScalarRowQuery(outer, valid))), rows.MapSelected(s.Nickname, expr.AsSelected(rows.ScalarRowQuery(outer, rows.SelectScalar(outer, expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(m.Name))))))))).Where(rows.ExistsQuery(foreign, outer)),
	} {
		state := &driverState{}
		got, err := rows.Read(q).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
	// Required scalar entry point rejects an already nullable input at construction.
	double := expr.AsSelected(rows.ScalarRowQuery(outer, nullable))
	if expr.FoundrySelected(double, sqlowner.Expressions()).Err == nil {
		t.Fatal("nested nullable scalar accepted")
	}
	var nilSource *rows.Input[member]
	if expr.FoundryPredicate(rows.ExistsQuery(outer, nilSource), sqlowner.Expressions()).Node == nil {
		t.Fatal("missing invalid subquery node")
	}
	state := &driverState{}
	if _, err := rows.Read(rows.Select(outer, s, rows.MapSelected(s.ID, expr.AsSelected(rows.ScalarRowQuery(outer, valid))), rows.MapSelected(s.Nickname, expr.AsSelected(rows.ScalarNullableRowQuery(outer, nullable)))).Where(rows.ExistsQuery(outer, nilSource))).All(t.Context(), openTest(t, state)); err == nil || state.sql != "" {
		t.Fatal("nil source executed", err)
	}
}
func TestScalarFiltersSnapshotAndSubqueryChecks(t *testing.T) {
	m := declareMembers()
	source := rows.From(m.Query())
	base := rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID)))))
	a := rows.WhereScalar(base, expr.Eq(expr.Value(orm.KeyExpr(m.ID)), 1))
	b := rows.WhereScalar(base, expr.Eq(expr.Value(orm.KeyExpr(m.ID)), 2))
	if len(rows.SourceOf(base).Node.Predicates) != 0 || len(rows.SourceOf(a).Node.Predicates) != 1 || len(rows.SourceOf(b).Node.Predicates) != 1 {
		t.Fatal("scalar filters mutated parent")
	}
	checks := 0
	rows.InputSource(&source).Checks = []func() error{func() error { checks++; return ormcore.Invalid("deferred inner declaration") }}
	bad := rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID)))))
	outer := rows.From(m.Query())
	schema := declareRows()
	q := rows.Select(outer, schema, rows.Map(schema.ID, expr.Value(orm.KeyExpr(m.ID))), rows.Map(schema.Name, expr.Value(orm.TextExpr(m.Name)))).Where(rows.InQuery(expr.Value(orm.KeyExpr(m.ID)), bad))
	if checks != 0 {
		t.Fatal("eager declaration check")
	}
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); err == nil || checks != 1 || state.sql != "" {
		t.Fatal(err, checks, state.sql)
	}
}

func TestSubqueryErrorsSurviveParametersAndDepthIsShared(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	source := rows.From(m.Query())
	invalid := rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(other.ID)))))
	zero := expr.ParamOf(rows.ScalarRowQuery(source, rows.Scalar[member, int64]{}), value.Null[int64]())
	if expr.FoundryRowValue(zero, sqlowner.Expressions()).Err == nil {
		t.Fatal("scalar parameter erased zero descriptor")
	}
	nullableZero := expr.ParamOf(rows.ScalarNullableRowQuery(source, rows.Scalar[member, value.Nullable[int64]]{}), value.Null[int64]())
	if expr.FoundryRowValue(nullableZero, sqlowner.Expressions()).Err == nil {
		t.Fatal("nullable scalar parameter erased zero descriptor")
	}
	p := expr.ParamOf(rows.ScalarRowQuery(source, invalid), value.Null[int64]())
	if expr.FoundryRowValue(p, sqlowner.Expressions()).Err == nil {
		t.Fatal("scalar parameter erased invalid source")
	}
	inner := rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID)))))
	for i := 0; i < querycore.MaxExpressionDepth; i++ {
		inner = rows.WhereScalar(rows.SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID))))), rows.InQuery(expr.Value(orm.KeyExpr(m.ID)), inner))
	}
	s := declareRows()
	q := rows.Select(source, s, rows.Map(s.ID, expr.Value(orm.KeyExpr(m.ID))), rows.Map(s.Name, expr.Value(orm.TextExpr(m.Name)))).Where(rows.InQuery(expr.Value(orm.KeyExpr(m.ID)), inner))
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("nested SELECT escaped shared bounds", err)
	}
}
