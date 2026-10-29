package rows

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
	Row[correlationCount]
	Count Field[correlationCount, value.Nullable[int64]]
}

func correlationCountSchema() correlationCountFields {
	return Define(func(b Builder[correlationCount], r *correlationCount) correlationCountFields {
		return correlationCountFields{b.Row(), NullableNumberOf(b, &r.Count)}
	})
}

func TestCorrelatedScalarMembershipAndExists(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	outer, inner := From(m.Query()), From(o.Query())
	c := Correlate(outer, inner)
	parent := expr.At(OuterScope(c, ModelScope(m.Query())), orm.KeyExpr(m.ID))
	child := expr.At(InnerScope(c, ModelScope(o.Query())), orm.NumberExpr(o.MemberID))
	c = c.Where(expr.Equal(child, parent))
	count := SelectCorrelatedScalar(c, expr.Count[Correlation[member, order]]().Value())
	ids := SelectCorrelatedScalar(c, expr.Row(child).Value())
	schema := correlationCountSchema()
	q := Select(outer, schema, MapSelected(schema.Count, CorrelatedScalarRowQuery(count).Value())).Where(c.Exists(), count.Exists(), InCorrelatedQuery(expr.Value(orm.KeyExpr(m.ID)), ids))
	state := &driverState{columns: []string{"count"}, rows: [][]driver.Value{{int64(2)}, {nil}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || got[0].Count != value.Of(int64(2)) || !got[1].Count.IsNull() {
		t.Fatal(got, err)
	}
	if strings.Count(state.sql, "SELECT") != 5 || !strings.Contains(state.sql, `"orders"."member_id" = "members"."id"`) {
		t.Fatal(state.sql)
	}
	if !strings.Contains(state.sql, `EXISTS(SELECT 1 FROM "orders"`) || !strings.Contains(state.sql, `EXISTS(SELECT COUNT(*) AS "value" FROM "orders"`) {
		t.Fatal("existence lost unselected or aggregate semantics", state.sql)
	}
	if len(c.core.Source.Node.Predicates) != 1 {
		t.Fatal("scalar modified correlation")
	}
}

func TestNestedCorrelationUsesOnlyEnclosingScopes(t *testing.T) {
	m := declareMembers()
	type third struct{}
	a, b, d := As[left](m.Query(), "a"), As[right](m.Query(), "b"), As[third](m.Query(), "d")
	first := Correlate(a, b)
	nested := Correlate(first, d)
	grandparent := expr.At(OuterScope(nested, OuterScope(first, Scope(a))), orm.KeyExpr(m.ID))
	parent := expr.At(OuterScope(nested, InnerScope(first, Scope(b))), orm.KeyExpr(m.ID))
	child := expr.At(InnerScope(nested, Scope(d)), orm.KeyExpr(m.ID))
	nested = nested.Where(expr.Greater(child, grandparent), expr.Less(child, parent)).Limit(1)
	first = first.Where(nested.Exists())
	schema := declareRows()
	q := Select(a, schema, Map(schema.ID, expr.At(Scope(a), orm.KeyExpr(m.ID))), Map(schema.Name, expr.At(Scope(a), orm.TextExpr(m.Name)))).Where(first.Exists())
	state := &driverState{columns: []string{"id", "name"}}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); err != nil {
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
	outer := From(m.Query())
	schema := correlationCountSchema()
	valid := Correlate(outer, From(o.Query()))
	other := Correlate(From(foreign.Query()), From(o.Query()))
	selected := SelectCorrelatedScalar(other, expr.Count[Correlation[member, order]]().Value())
	invalidScope := OuterScope(valid, ModelScope(foreign.Query()))
	if !errors.Is(expr.FoundryScopeData(invalidScope, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("foreign scope lifted")
	}
	var missing *Input[member]
	invalid := []CorrelatedSource[member, order]{Correlate(missing, From(o.Query())), CorrelatedSource[member, order]{}}
	for _, c := range invalid {
		if !errors.Is(expr.FoundryPredicate(c.Exists(), sqlowner.Expressions()).Err, fault.Invalid) {
			t.Fatal("zero correlation accepted")
		}
	}
	shadow := Correlate(As[left](m.Query(), "same"), As[right](m.Query(), "same"))
	if !errors.Is(expr.FoundryPredicate(shadow.Exists(), sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("shadowing accepted")
	}
	for _, q := range []Query[member, correlationCount]{
		Select(outer, schema, MapSelected(schema.Count, CorrelatedScalarRowQuery(selected).Value())),
		Select(outer, schema, MapSelected(schema.Count, CorrelatedScalarRowQuery(SelectCorrelatedScalar(valid, expr.Count[Correlation[member, order]]().Value())).Value())).Where(other.Exists()),
	} {
		state := &driverState{}
		if got, err := Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
	zero := CorrelatedScalarRowQuery(CorrelatedScalar[member, order, int64]{}).Param(value.Null[int64]())
	if !errors.Is(expr.FoundryRowValue(zero, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("Param erased zero correlation")
	}
}

func TestCorrelationNullableScopesAndDeferredChecks(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	j := LeftJoin(a, b, On(expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))))
	c := Correlate(j, From(o.Query()))
	nullable := OuterNullableScope(c, NullableRightScope(j, Scope(b)))
	v := expr.AtNullable(nullable, orm.KeyExpr(m.ID))
	if expr.FoundrySelectedColumn(expr.Row(v).Value(), sqlowner.Expressions()).Nullable != true {
		t.Fatal("outer nullability lost")
	}
	wrong := OuterNullableScope(c, NullableRightScope(j, Scope(As[right](declareMembers().Query(), "b"))))
	if !errors.Is(expr.FoundryNullableScopeData(wrong, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("foreign nullable scope accepted")
	}
	checks := 0
	c.core.Source.Checks = append(c.core.Source.Checks, func() error { checks++; return ormcore.Invalid("deferred declaration") })
	schema := declareRows()
	q := Select(j, schema, Map(schema.ID, expr.At(LeftScope(j, Scope(a)), orm.KeyExpr(m.ID))), Map(schema.Name, expr.At(LeftScope(j, Scope(a)), orm.TextExpr(m.Name)))).Where(c.Exists())
	if checks != 0 {
		t.Fatal("eager validation")
	}
	state := &driverState{}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || checks != 1 || state.sql != "" {
		t.Fatal(err, checks, state.sql)
	}
}
