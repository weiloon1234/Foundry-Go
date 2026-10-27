package rows

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
	outer := From(m.Query())
	inner := SelectScalar(From(o.Query().Where(o.MemberID.Gt(4)).Limit(1)), expr.Row(expr.Value(o.ID)).Value())
	nullable := SelectScalar(From(m.Query().Where(m.ID.Eq(1))), expr.Row(expr.Value(m.Nickname)).Value())
	s := outerSchema()
	q := Select(outer, s, MapSelected(s.ID, ScalarRowQuery(outer, inner).Value()), MapSelected(s.Nickname, ScalarNullableRowQuery(outer, nullable).Value())).Where(ExistsQuery(outer, inner), InQuery(expr.Value(m.ID), inner), expr.Not(InNullableQuery(expr.Value(m.Name), nullable)))
	state := &driverState{columns: []string{"id", "nickname"}, rows: [][]driver.Value{{nil, nil}, {int64(9), "nick"}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].ID.IsNull() || !got[0].Nickname.IsNull() || got[1].ID != value.Of(int64(9)) {
		t.Fatal(got, err)
	}
	if strings.Count(state.sql, "SELECT") != 6 || !strings.Contains(state.sql, "EXISTS(") || !strings.Contains(state.sql, " IN (") || !strings.Contains(state.sql, "NOT ") {
		t.Fatal(state.sql)
	}
	// Inner aggregate ownership does not turn its scalar result into an outer aggregate.
	count := SelectScalar(From(o.Query()), expr.Count[order]().Value())
	row := ScalarRowQuery(outer, count)
	core := expr.FoundryRowValue(row, sqlowner.Expressions())
	if _, ok := core.Node.(querycore.ScalarSubquery); !ok {
		t.Fatalf("lost SELECT boundary: %T", core.Node)
	}
	if err := ormcore.CheckReferences(outer.source, core.Refs); err != nil {
		t.Fatal(err)
	}
}
func TestSubqueriesRetainErrorsAndOuterDeclarations(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	outer := From(m.Query())
	foreign := From(other.Query())
	valid := SelectScalar(outer, expr.Row(expr.Value(m.ID)).Value())
	bad := SelectScalar(outer, expr.Row(expr.Value(other.ID)).Value())
	nullable := SelectScalar(outer, expr.Row(expr.Value(m.Nickname)).Value())
	s := outerSchema()
	for _, q := range []Query[member, outerRecord]{
		Select(outer, s, MapSelected(s.ID, ScalarRowQuery(outer, bad).Value()), MapSelected(s.Nickname, ScalarNullableRowQuery(outer, nullable).Value())),
		Select(outer, s, MapSelected(s.ID, ScalarRowQuery(foreign, valid).Value()), MapSelected(s.Nickname, ScalarNullableRowQuery(outer, nullable).Value())),
		Select(outer, s, MapSelected(s.ID, ScalarRowQuery(outer, valid).Value()), MapSelected(s.Nickname, ScalarRowQuery(outer, SelectScalar(outer, expr.Row(expr.Value(m.Name)).Value())).Value())).Where(ExistsQuery(foreign, outer)),
	} {
		state := &driverState{}
		got, err := Read(q).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
	// Required scalar entry point rejects an already nullable input at construction.
	double := ScalarRowQuery(outer, nullable).Value()
	if expr.FoundrySelected(double, sqlowner.Expressions()).Err == nil {
		t.Fatal("nested nullable scalar accepted")
	}
	var nilSource *Input[member]
	if expr.FoundryPredicate(ExistsQuery(outer, nilSource), sqlowner.Expressions()).Node == nil {
		t.Fatal("missing invalid subquery node")
	}
	state := &driverState{}
	if _, err := Read(Select(outer, s, MapSelected(s.ID, ScalarRowQuery(outer, valid).Value()), MapSelected(s.Nickname, ScalarNullableRowQuery(outer, nullable).Value())).Where(ExistsQuery(outer, nilSource))).All(t.Context(), openTest(t, state)); err == nil || state.sql != "" {
		t.Fatal("nil source executed", err)
	}
}
func TestScalarFiltersSnapshotAndSubqueryChecks(t *testing.T) {
	m := declareMembers()
	source := From(m.Query())
	base := SelectScalar(source, expr.Row(expr.Value(m.ID)).Value())
	a := WhereScalar(base, expr.Value(m.ID).Eq(1))
	b := WhereScalar(base, expr.Value(m.ID).Eq(2))
	if len(base.source.Node.Predicates) != 0 || len(a.source.Node.Predicates) != 1 || len(b.source.Node.Predicates) != 1 {
		t.Fatal("scalar filters mutated parent")
	}
	checks := 0
	source.source.Checks = []func() error{func() error { checks++; return ormcore.Invalid("deferred inner declaration") }}
	bad := SelectScalar(source, expr.Row(expr.Value(m.ID)).Value())
	outer := From(m.Query())
	schema := declareRows()
	q := Select(outer, schema, Map(schema.ID, expr.Value(m.ID)), Map(schema.Name, expr.Value(m.Name))).Where(InQuery(expr.Value(m.ID), bad))
	if checks != 0 {
		t.Fatal("eager declaration check")
	}
	state := &driverState{}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); err == nil || checks != 1 || state.sql != "" {
		t.Fatal(err, checks, state.sql)
	}
}

func TestSubqueryErrorsSurviveParametersAndDepthIsShared(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	source := From(m.Query())
	invalid := SelectScalar(source, expr.Row(expr.Value(other.ID)).Value())
	zero := ScalarRowQuery(source, Scalar[member, int64]{}).Param(value.Null[int64]())
	if expr.FoundryRowValue(zero, sqlowner.Expressions()).Err == nil {
		t.Fatal("scalar parameter erased zero descriptor")
	}
	nullableZero := ScalarNullableRowQuery(source, Scalar[member, value.Nullable[int64]]{}).Param(value.Null[int64]())
	if expr.FoundryRowValue(nullableZero, sqlowner.Expressions()).Err == nil {
		t.Fatal("nullable scalar parameter erased zero descriptor")
	}
	p := ScalarRowQuery(source, invalid).Param(value.Null[int64]())
	if expr.FoundryRowValue(p, sqlowner.Expressions()).Err == nil {
		t.Fatal("scalar parameter erased invalid source")
	}
	inner := SelectScalar(source, expr.Row(expr.Value(m.ID)).Value())
	for i := 0; i < querycore.MaxExpressionDepth; i++ {
		inner = WhereScalar(SelectScalar(source, expr.Row(expr.Value(m.ID)).Value()), InQuery(expr.Value(m.ID), inner))
	}
	s := declareRows()
	q := Select(source, s, Map(s.ID, expr.Value(m.ID)), Map(s.Name, expr.Value(m.Name))).Where(InQuery(expr.Value(m.ID), inner))
	state := &driverState{}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal("nested SELECT escaped shared bounds", err)
	}
}
