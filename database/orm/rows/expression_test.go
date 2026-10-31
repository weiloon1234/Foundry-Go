package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	rows.Row[expressionRow[V]]
	Value rows.Field[expressionRow[V], V]
}

func expressionSchema[V any](c codec.Codec[V]) expressionFields[V] {
	return rows.Define(func(b rows.Builder[expressionRow[V]], r *expressionRow[V]) expressionFields[V] {
		return expressionFields[V]{b.Row(), rows.FieldOf(b, &r.Value, orm.Codec(c))}
	})
}
func expressionQuery[S, V any](source rows.SelectionSource[S], v expr.Selected[S, V], c codec.Codec[V]) rows.Query[S, expressionRow[V]] {
	schema := expressionSchema(c)
	return rows.Select(source, schema, rows.MapSelected(schema.Value, v))
}
func expressionSQL[S, R any](t *testing.T, q rows.Query[S, R]) querycore.Statement {
	t.Helper()
	s, err := querycore.CompileRead(rows.SourceOf(q).Node, context.Background(), false, rows.SourceOf(q).Err, true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestExpressionParametersAndNullableProjection(t *testing.T) {
	m := declareMembers()
	a := rows.As[left](m.Query(), "m")
	n := expr.At(rows.Scope(a), orm.NullableTextExpr(m.Nickname))
	s := expressionSchema(codec.Nullable(codec.String[string]()))
	q := rows.Select(a, s, rows.MapSelected(s.Value, expr.AsSelected(expr.Row(n))))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"Ada"}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
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
	a := rows.As[right](o.Query(), "o")
	id := expr.At(rows.Scope(a), orm.NumberExpr(o.MemberID))
	input := expr.Add(id, id)
	q := rows.OrderBy(expressionQuery(a, expr.AsSelected(expr.Divide(input, expr.ParamOf(input, 2))), codec.Signed[int64]()).Where(expr.Gt(input, 0)), expr.Asc(expr.Negate(id)))
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, " / ") || !strings.Contains(s.SQLText, "ORDER BY") || !reflect.DeepEqual(s.Bindings, []any{int64(2), int64(0)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(3)}}}
	got, err := rows.Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 3 {
		t.Fatal(got, err)
	}
	null := expr.ParamOf(expr.NullableRow(id), value.Null[int64]())
	nullable := expressionQuery(a, expr.AsSelected(expr.AddNullable(expr.NullableRow(id), null)), codec.Nullable(codec.Signed[int64]()))
	statement := expressionSQL(t, nullable)
	if !reflect.DeepEqual(statement.Bindings, []any{nil}) {
		t.Fatal(statement.Bindings)
	}
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}}}
	result, err := rows.Read(nullable).All(t.Context(), openTest(t, state))
	if err != nil || len(result) != 1 || !result[0].Value.IsNull() {
		t.Fatal(result, err)
	}
}
func TestReferenceFreeCountAndExists(t *testing.T) {
	o := declareOrders()
	a := rows.As[right](o.Query(), "o")
	count := expressionQuery(a, expr.AsSelected(expr.Count[rows.Alias[right, order]]()), codec.Signed[int64]())
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(0)}}}
	got, err := rows.Read(count).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 0 || !strings.Contains(state.sql, "COUNT(*)") {
		t.Fatal(got, err, state.sql)
	}
	exists := expressionQuery(a, expr.AsSelected(expr.Exists[rows.Alias[right, order]]()), codec.Bool[bool]())
	state = &driverState{columns: []string{"value"}, rows: [][]driver.Value{{false}}}
	result, err := rows.Read(exists).All(t.Context(), openTest(t, state))
	if err != nil || len(result) != 1 || result[0].Value || !strings.Contains(state.sql, "COUNT(*) > 0") {
		t.Fatal(result, err, state.sql)
	}
}
func TestExpressionRejectsZeroAndForeignDeclarations(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	a := rows.As[right](o.Query(), "o")
	own, foreign := expr.At(rows.Scope(a), orm.NumberExpr(o.MemberID)), expr.At(rows.Scope(a), orm.NumberExpr(other.MemberID))
	var missing expr.RowExpression[rows.Alias[right, order], int64]
	for _, v := range []expr.Selected[rows.Alias[right, order], int64]{expr.AsSelected(expr.Add(own, foreign)), expr.AsSelected(expr.Add(own, missing)), {}} {
		state := &driverState{}
		got, err := rows.Read(expressionQuery(a, v, codec.Signed[int64]())).All(t.Context(), openTest(t, state))
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
	p := expr.Param(orm.ColumnExpr(d.Data), payload)
	payload[0] = 'X'
	core := expr.FoundrySelected(expr.AsSelected(p), sqlowner.Expressions())
	c := querycore.NewCompiler(nil)
	sql, err := c.SelectedExpression(core.Node, nil, false)
	if err != nil || sql != "CAST($1 AS bytea)" || !reflect.DeepEqual(c.Arguments, []any{[]byte("before")}) {
		t.Fatal(sql, err, c.Arguments)
	}
	o := declareOrders()
	source := rows.As[right](o.Query(), "o")
	v := expr.At(rows.Scope(source), orm.NumberExpr(o.MemberID))
	exact := expressionQuery(source, expr.AsSelected(expr.DecimalOf(v)), codec.Decimal())
	s := expressionSQL(t, exact)
	if !strings.Contains(s.SQLText, " AS numeric)") {
		t.Fatal(s.SQLText)
	}
	approx := expressionQuery(source, expr.AsSelected(expr.FloatOf(v)), codec.Float[float64]())
	s = expressionSQL(t, approx)
	if !strings.Contains(s.SQLText, " AS double precision)") {
		t.Fatal(s.SQLText)
	}
}

func TestModelComputedRowClauses(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	amount := expr.Value(orm.NumberExpr(o.MemberID))
	q := expr.OrderBy(expr.Where(o.Query(), expr.GreaterColumn(orm.NumberExpr(o.MemberID), orm.NumberExpr(o.MemberID)), expr.Gt(expr.Add(amount, amount), 0)), expr.Desc(expr.Negate(amount)))
	state := &driverState{columns: []string{"id", "member_id"}, rows: [][]driver.Value{{int64(1), int64(2)}}}
	got, err := orm.All(t.Context(), openTest(t, state), q)
	if err != nil || len(got) != 1 || got[0].ID != 1 || !strings.Contains(state.sql, "ORDER BY") {
		t.Fatal(got, err, state.sql)
	}
	for _, invalid := range []orm.Query[order]{expr.Where(o.Query(), expr.Eq(expr.Value(orm.NumberExpr(other.MemberID)), 1)), expr.OrderBy(o.Query(), expr.Asc(expr.Negate(expr.Value(orm.NumberExpr(other.MemberID))))), expr.Where(o.Query(), expr.Predicate[order]{})} {
		state := &driverState{}
		out, err := orm.All(t.Context(), openTest(t, state), invalid)
		if err == nil || out != nil || state.sql != "" {
			t.Fatal(out, err, state.sql)
		}
	}
}

func TestParameterOwnershipSurvivesReferenceFreeConstruction(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	m := declareMembers()
	direct := expr.FoundryPredicate(expr.Eq(expr.Param(orm.NumberExpr(o.MemberID), 1), 1), sqlowner.Expressions())
	count := expr.FoundryPredicate(expr.Eq(expr.ParamOf(expr.Count[order](), 1), 1), sqlowner.Expressions())
	cast := expr.FoundryPredicate(expr.Eq(expr.ParamOf(expr.DecimalOf(expr.Value(orm.NumberExpr(o.MemberID))), decimal.FromInt64(1)), decimal.FromInt64(1)), sqlowner.Expressions())
	for _, predicate := range []ormcore.Predicate{direct, count, cast} {
		if !predicate.ReferenceFree || len(predicate.Refs) == 0 {
			t.Fatal("parameter lost construction ownership", predicate)
		}
		state := &driverState{}
		got, err := orm.All(t.Context(), openTest(t, state), orm.FoundryWhere(m.Query(), predicate, sqlowner.Queries()))
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("foreign model accepted parameter", got, err, state.sql)
		}
	}
	state := &driverState{}
	got, err := orm.All(t.Context(), openTest(t, state), orm.FoundryWhere(other.Query(), direct, sqlowner.Queries()))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("foreign declaration accepted parameter", got, err, state.sql)
	}
}

func TestExpressionReferencesStayBoundedAfter64SelfDoublings(t *testing.T) {
	o := declareOrders()
	x := expr.OrderRow(expr.Value(orm.NumberExpr(o.MemberID)))
	for range 64 {
		x = expr.Add(x, x)
		c := expr.FoundrySelected(expr.AsSelected(x), sqlowner.Expressions())
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
	source := rows.As[right](o.Query(), "o")
	foreign := expr.At(rows.Scope(source), orm.NumberExpr(other.MemberID))
	for name, selected := range map[string]expr.Selected[rows.Alias[right, order], int64]{
		"row":      expr.AsSelected(expr.ParamOf(expr.Add(foreign, foreign), 1)),
		"selected": expr.AsSelected(expr.ParamOf(expr.AsSelected(expr.Row(foreign)), 1)),
		"ordered":  expr.AsSelected(expr.ParamOf(expr.OrderRow(foreign), 1)),
		"chained":  expr.AsSelected(expr.ParamOf(expr.ParamOf(expr.AsSelected(expr.Row(foreign)), 1), 2)),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			got, err := rows.Read(expressionQuery(source, selected, codec.Signed[int64]())).All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("Param laundered foreign scope", got, err, state.sql)
			}
		})
	}
	nullable := expr.ParamOf(expr.OrderNullableRow(expr.NullableRow(foreign)), value.Of(int64(1)))
	state := &driverState{}
	got, err := rows.Read(expressionQuery(source, expr.AsSelected(nullable), codec.Nullable(codec.Signed[int64]()))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("nullable Param laundered foreign scope", got, err, state.sql)
	}
	cast := expr.ParamOf(expr.DecimalOf(foreign), decimal.FromInt64(1))
	state = &driverState{}
	exact, err := rows.Read(expressionQuery(source, expr.AsSelected(cast), codec.Decimal())).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || exact != nil || state.sql != "" {
		t.Fatal("cast Param laundered foreign scope", exact, err, state.sql)
	}
}
