package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type expressionRow[V any] struct{ Value V }
type expressionFields[V any] struct {
	Row[expressionRow[V]]
	Value Field[expressionRow[V], V]
}

func expressionSchema[V any](c codec.Codec[V]) expressionFields[V] {
	return Define(func(b Builder[expressionRow[V]], r *expressionRow[V]) expressionFields[V] {
		return expressionFields[V]{b.Row(), FieldOf(b, &r.Value, orm.Codec(c))}
	})
}
func expressionQuery[S, V any](source Source[S], v expr.Selected[S, V], c codec.Codec[V]) Query[S, expressionRow[V]] {
	schema := expressionSchema(c)
	return Select(source, schema, MapSelected(schema.Value, v))
}
func expressionSQL[S, R any](t *testing.T, q Query[S, R]) querycore.Statement {
	t.Helper()
	s, err := querycore.CompileRead(q.source.Node, context.Background(), false, q.source.Err, true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestExpressionParametersAndNullableProjection(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query(), "m")
	n := expr.At(Scope(a), m.Nickname)
	s := expressionSchema(codec.Nullable(codec.String[string]()))
	q := Select(a, s, MapSelected(s.Value, expr.Select(n)))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"Ada"}}}
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
		t.Fatal(got, err)
	}
	v, ok := got[1].Value.Get()
	if !ok || v != "Ada" {
		t.Fatal(got)
	}
}
func TestExpressionArithmeticAndParameters(t *testing.T) {
	o := declareOrders()
	a := As[right](o.Query(), "o")
	id := expr.At(Scope(a), o.MemberID)
	input := expr.Add(id, id)
	q := expressionQuery(a, expr.Divide(input, input.Param(2)).Value(), codec.Signed[int64]()).Where(input.Gt(0)).OrderBy(expr.Negate(id).Asc())
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, " / ") || !strings.Contains(s.SQLText, "ORDER BY") || !reflect.DeepEqual(s.Bindings, []any{int64(2), int64(0)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(3)}}}
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 3 {
		t.Fatal(got, err)
	}
	null := expr.NullFor(id)
	nullable := expressionQuery(a, expr.AddNullable(expr.NullableRow(id), null).Value(), codec.Nullable(codec.Signed[int64]()))
	statement := expressionSQL(t, nullable)
	if !reflect.DeepEqual(statement.Bindings, []any{nil}) {
		t.Fatal(statement.Bindings)
	}
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}}}
	result, err := nullable.All(t.Context(), openTest(t, state))
	if err != nil || len(result) != 1 || !result[0].Value.IsNull() {
		t.Fatal(result, err)
	}
}
func TestReferenceFreeCountAndExists(t *testing.T) {
	o := declareOrders()
	a := As[right](o.Query(), "o")
	count := expressionQuery(a, expr.Count[Alias[right, order]]().Value(), codec.Signed[int64]())
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(0)}}}
	got, err := count.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 0 || !strings.Contains(state.sql, "COUNT(*)") {
		t.Fatal(got, err, state.sql)
	}
	exists := expressionQuery(a, expr.Exists[Alias[right, order]]().Value(), codec.Bool[bool]())
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{false}}}
	result, err := exists.All(t.Context(), openTest(t, state))
	if err != nil || len(result) != 1 || result[0].Value || !strings.Contains(state.sql, "COUNT(*) > 0") {
		t.Fatal(result, err, state.sql)
	}
}
func TestExpressionRejectsZeroAndForeignDeclarations(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	a := As[right](o.Query(), "o")
	own, foreign := expr.At(Scope(a), o.MemberID), expr.At(Scope(a), other.MemberID)
	var missing expr.RowExpression[Alias[right, order], int64]
	for _, v := range []expr.Selected[Alias[right, order], int64]{expr.Add(own, foreign).Value(), expr.Add(own, missing).Value(), {}} {
		state := &driverState{}
		got, err := expressionQuery(a, v, codec.Signed[int64]()).All(t.Context(), openTest(t, state))
		if err == nil || got != nil || state.sql != "" {
			t.Fatal(got, err, state.sql)
		}
	}
}
func TestParameterSnapshotsAndCasts(t *testing.T) {
	type document struct {
		ID   int64
		Data []byte
	}
	type declaration struct {
		orm.Table[document, int64]
		Data orm.Column[document, []byte]
	}
	d := orm.Define("documents", func(b orm.Builder[document], r *document) declaration {
		return declaration{orm.TableOf(b, &r.ID), orm.BytesOf(b, &r.Data)}
	})
	payload := []byte("before")
	p := expr.Param(d.Data, payload)
	payload[0] = 'X'
	core := expr.FoundrySelected(p.Value(), sqlowner.Expressions())
	c := querycore.NewCompiler(nil)
	sql, err := c.SelectedExpression(core.Node, nil, false)
	if err != nil || sql != "CAST($1 AS bytea)" || !reflect.DeepEqual(c.Arguments, []any{[]byte("before")}) {
		t.Fatal(sql, err, c.Arguments)
	}
	o := declareOrders()
	source := As[right](o.Query(), "o")
	v := expr.At(Scope(source), o.MemberID)
	exact := expressionQuery(source, expr.DecimalOf(v).Value(), codec.Decimal())
	s := expressionSQL(t, exact)
	if !strings.Contains(s.SQLText, " AS numeric)") {
		t.Fatal(s.SQLText)
	}
	approx := expressionQuery(source, expr.FloatOf(v).Value(), codec.Float[float64]())
	s = expressionSQL(t, approx)
	if !strings.Contains(s.SQLText, " AS double precision)") {
		t.Fatal(s.SQLText)
	}
}

func TestModelComputedRowClauses(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	amount := expr.Value(o.MemberID)
	q := expr.OrderBy(expr.Where(o.Query(), expr.GreaterColumn(o.MemberID, o.MemberID), expr.Add(amount, amount).Gt(0)), expr.Negate(amount).Desc())
	state := &driverState{columns: []string{"id", "member_id"}, rows: [][]driver.Value{{int64(1), int64(2)}}}
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].ID != 1 || !strings.Contains(state.sql, "ORDER BY") {
		t.Fatal(got, err, state.sql)
	}
	for _, invalid := range []orm.Query[order]{expr.Where(o.Query(), expr.Value(other.MemberID).Eq(1)), expr.OrderBy(o.Query(), expr.Negate(expr.Value(other.MemberID)).Asc()), expr.Where(o.Query(), expr.Predicate[order]{})} {
		state := &driverState{}
		out, err := invalid.All(t.Context(), openTest(t, state))
		if err == nil || out != nil || state.sql != "" {
			t.Fatal(out, err, state.sql)
		}
	}
}

func TestParameterOwnershipSurvivesReferenceFreeConstruction(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	m := declareMembers()
	direct := expr.FoundryPredicate(expr.Param(o.MemberID, 1).Eq(1), sqlowner.Expressions())
	count := expr.FoundryPredicate(expr.Count[order]().Param(1).Eq(1), sqlowner.Expressions())
	cast := expr.FoundryPredicate(expr.DecimalOf(expr.Value(o.MemberID)).Param(decimal.FromInt64(1)).Eq(decimal.FromInt64(1)), sqlowner.Expressions())
	for _, predicate := range []ormcore.Predicate{direct, count, cast} {
		if !predicate.ReferenceFree || len(predicate.Refs) == 0 {
			t.Fatal("parameter lost construction ownership", predicate)
		}
		state := &driverState{}
		got, err := orm.FoundryWhere(m.Query(), predicate, sqlowner.Queries()).All(t.Context(), openTest(t, state))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("foreign model accepted parameter", got, err, state.sql)
		}
	}
	state := &driverState{}
	got, err := orm.FoundryWhere(other.Query(), direct, sqlowner.Queries()).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("foreign declaration accepted parameter", got, err, state.sql)
	}
}

func TestExpressionReferencesStayBoundedAfter64SelfDoublings(t *testing.T) {
	o := declareOrders()
	x := expr.OrderRow(expr.Value(o.MemberID))
	for range 64 {
		x = expr.Add(x, x)
		c := expr.FoundrySelected(x.Value(), sqlowner.Expressions())
		if c.Err != nil || len(c.Refs) != 1 {
			t.Fatal("expression references grew", len(c.Refs), c.Err)
		}
	}
	comparison := expr.Equal(x, x)
	for range 64 {
		comparison = expr.And(comparison, comparison)
		p := expr.FoundryPredicate(comparison, sqlowner.Expressions())
		if p.Err != nil || len(p.Refs) != 1 {
			t.Fatal("predicate references grew", len(p.Refs), p.Err)
		}
	}
}

func TestParamPreservesInvalidScopedExemplar(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	source := As[right](o.Query(), "o")
	foreign := expr.At(Scope(source), other.MemberID)
	for name, selected := range map[string]expr.Selected[Alias[right, order], int64]{
		"row":      expr.Add(foreign, foreign).Param(1).Value(),
		"selected": expr.Select(foreign).Param(1).Value(),
		"ordered":  expr.OrderRow(foreign).Param(1).Value(),
		"chained":  expr.Select(foreign).Param(1).Param(2).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			got, err := expressionQuery(source, selected, codec.Signed[int64]()).All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("Param laundered foreign scope", got, err, state.sql)
			}
		})
	}
	nullable := expr.OrderNullableRow(expr.NullableRow(foreign)).Param(value.Of(int64(1)))
	state := &driverState{}
	got, err := expressionQuery(source, nullable.Value(), codec.Nullable(codec.Signed[int64]())).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("nullable Param laundered foreign scope", got, err, state.sql)
	}
	cast := expr.DecimalOf(foreign).Param(decimal.FromInt64(1))
	state = &driverState{}
	exact, err := expressionQuery(source, cast.Value(), codec.Decimal()).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || exact != nil || state.sql != "" {
		t.Fatal("cast Param laundered foreign scope", exact, err, state.sql)
	}
}
