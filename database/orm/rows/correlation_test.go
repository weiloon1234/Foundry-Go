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
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type correlationCount struct{ Count value.Nullable[int64] }
type correlationCountFields struct {
	rows.Row[correlationCount]
	Count rows.Field[correlationCount, value.Nullable[int64]]
}

func correlationCountSchema() correlationCountFields {
	return rows.Define(func(b rows.Builder[correlationCount], r *correlationCount) correlationCountFields {
		return correlationCountFields{b.Row(), rows.NullableNumberOf(b, &r.Count)}
	})
}

func TestCorrelatedScalarMembershipAndExists(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	outer, inner := rows.From(m.Query()), rows.From(o.Query())
	c := rows.Correlate(outer, inner)
	parent := expr.At(rows.OuterScope(c, rows.ModelScope(m.Query())), orm.KeyExpr(m.ID))
	child := expr.At(rows.InnerScope(c, rows.ModelScope(o.Query())), orm.NumberExpr(o.MemberID))
	c = c.Where(expr.Equal(child, parent))
	count := rows.SelectCorrelatedScalar(c, expr.Count[rows.Correlation[member, order]]().Value())
	ids := rows.SelectCorrelatedScalar(c, expr.Row(child).Value())
	schema := correlationCountSchema()
	q := rows.Select(outer, schema, rows.MapSelected(schema.Count, rows.CorrelatedScalarRowQuery(count).Value())).Where(c.Exists(), count.Exists(), rows.InCorrelatedQuery(expr.Value(orm.KeyExpr(m.ID)), ids))
	state := &driverState{columns: []string{"count"}, rows: [][]driver.Value{{int64(2)}, {nil}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || got[0].Count != value.Of(int64(2)) || !got[1].Count.IsNull() {
		t.Fatal(got, err)
	}
	if strings.Count(state.sql, "SELECT") != 5 || !strings.Contains(state.sql, `"orders"."member_id" = "members"."id"`) {
		t.Fatal(state.sql)
	}
	if !strings.Contains(state.sql, `EXISTS(SELECT 1 FROM "orders"`) || !strings.Contains(state.sql, `EXISTS(SELECT COUNT(*) AS "value" FROM "orders"`) {
		t.Fatal("existence lost unselected or aggregate semantics", state.sql)
	}
	if len(rows.CorrelatedSourceCore(&c).Source.Node.Predicates) != 1 {
		t.Fatal("scalar modified correlation")
	}
}

func TestNestedCorrelationUsesOnlyEnclosingScopes(t *testing.T) {
	m := declareMembers()
	type third struct{}
	a, b, d := rows.As[left](m.Query(), "a"), rows.As[right](m.Query(), "b"), rows.As[third](m.Query(), "d")
	first := rows.Correlate(a, b)
	nested := rows.Correlate(first, d)
	grandparent := expr.At(rows.OuterScope(nested, rows.OuterScope(first, rows.Scope(a))), orm.KeyExpr(m.ID))
	parent := expr.At(rows.OuterScope(nested, rows.InnerScope(first, rows.Scope(b))), orm.KeyExpr(m.ID))
	child := expr.At(rows.InnerScope(nested, rows.Scope(d)), orm.KeyExpr(m.ID))
	nested = nested.Where(expr.Greater(child, grandparent), expr.Less(child, parent)).Limit(1)
	first = first.Where(nested.Exists())
	schema := declareRows()
	q := rows.Select(a, schema, rows.Map(schema.ID, expr.At(rows.Scope(a), orm.KeyExpr(m.ID))), rows.Map(schema.Name, expr.At(rows.Scope(a), orm.TextExpr(m.Name)))).Where(first.Exists())
	state := &driverState{columns: []string{"id", "name"}}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"d"."id" > "a"."id"`, `"d"."id" < "b"."id"`, `LIMIT $1`} {
		if !strings.Contains(state.sql, part) {
			t.Fatal(state.sql)
		}
	}
	if strings.Count(state.sql, "SELECT") != 3 {
		t.Fatal(state.sql)
	}
}

func TestCorrelationRejectsForeignOwnersShadowingAndZeroDescriptors(t *testing.T) {
	m, foreign, o := declareMembers(), declareMembers(), declareOrders()
	outer := rows.From(m.Query())
	schema := correlationCountSchema()
	valid := rows.Correlate(outer, rows.From(o.Query()))
	other := rows.Correlate(rows.From(foreign.Query()), rows.From(o.Query()))
	selected := rows.SelectCorrelatedScalar(other, expr.Count[rows.Correlation[member, order]]().Value())
	invalidScope := rows.OuterScope(valid, rows.ModelScope(foreign.Query()))
	if !errors.Is(expr.FoundryScopeData(invalidScope, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("foreign scope lifted")
	}
	var missing *rows.Input[member]
	invalid := []rows.CorrelatedSource[member, order]{rows.Correlate(missing, rows.From(o.Query())), rows.CorrelatedSource[member, order]{}}
	for _, c := range invalid {
		if !errors.Is(expr.FoundryPredicate(c.Exists(), sqlowner.Expressions()).Err, fault.Invalid) {
			t.Fatal("zero correlation accepted")
		}
	}
	shadow := rows.Correlate(rows.As[left](m.Query(), "same"), rows.As[right](m.Query(), "same"))
	if !errors.Is(expr.FoundryPredicate(shadow.Exists(), sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("shadowing accepted")
	}
	for _, q := range []rows.Query[member, correlationCount]{
		rows.Select(outer, schema, rows.MapSelected(schema.Count, rows.CorrelatedScalarRowQuery(selected).Value())),
		rows.Select(outer, schema, rows.MapSelected(schema.Count, rows.CorrelatedScalarRowQuery(rows.SelectCorrelatedScalar(valid, expr.Count[rows.Correlation[member, order]]().Value())).Value())).Where(other.Exists()),
	} {
		state := &driverState{}
		if got, err := rows.Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
	zero := rows.CorrelatedScalarRowQuery(rows.CorrelatedScalar[member, order, int64]{}).Param(value.Null[int64]())
	if !errors.Is(expr.FoundryRowValue(zero, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("Param erased zero correlation")
	}
}

func TestCorrelationNullableScopesAndDeferredChecks(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := rows.As[left](m.Query(), "a"), rows.As[right](m.Query(), "b")
	j := rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.KeyExpr(m.ID))))
	c := rows.Correlate(j, rows.From(o.Query()))
	nullable := rows.OuterNullableScope(c, rows.NullableRightScope(j, rows.Scope(b)))
	v := expr.AtNullable(nullable, orm.KeyExpr(m.ID))
	if expr.FoundrySelectedColumn(expr.Row(v).Value(), sqlowner.Expressions()).Nullable != true {
		t.Fatal("outer nullability lost")
	}
	wrong := rows.OuterNullableScope(c, rows.NullableRightScope(j, rows.Scope(rows.As[right](declareMembers().Query(), "b"))))
	if !errors.Is(expr.FoundryNullableScopeData(wrong, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("foreign nullable scope accepted")
	}
	checks := 0
	rows.CorrelatedSourceCore(&c).Source.Checks = append(rows.CorrelatedSourceCore(&c).Source.Checks, func() error { checks++; return ormcore.Invalid("deferred declaration") })
	schema := declareRows()
	q := rows.Select(j, schema, rows.Map(schema.ID, expr.At(rows.LeftScope(j, rows.Scope(a)), orm.KeyExpr(m.ID))), rows.Map(schema.Name, expr.At(rows.LeftScope(j, rows.Scope(a)), orm.TextExpr(m.Name)))).Where(c.Exists())
	if checks != 0 {
		t.Fatal("eager validation")
	}
	state := &driverState{}
	if _, err := rows.Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || checks != 1 || state.sql != "" {
		t.Fatal(err, checks, state.sql)
	}
}
