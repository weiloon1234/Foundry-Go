package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
)

func TestValueComparisonsShareSQLAndBindings(t *testing.T) {
	m := declareMembers()
	id := expr.Value(orm.KeyExpr(m.ID))
	v := expr.Param(orm.KeyExpr(m.ID), 2)
	for _, test := range []struct {
		p  expr.Predicate[member]
		op string
	}{
		{expr.Equal(id, v), "="}, {expr.NotEqual(id, v), "<>"}, {expr.Less(id, v), "<"}, {expr.LessOrEqual(id, v), "<="}, {expr.Greater(id, v), ">"}, {expr.GreaterOrEqual(id, v), ">="}, {expr.DistinctFrom(id, v), "IS DISTINCT FROM"}, {expr.NotDistinctFrom(id, v), "IS NOT DISTINCT FROM"},
	} {
		s, e := orm.Compile(expr.Where(m.Query(), test.p))
		if e != nil || !strings.Contains(s.SQL(), test.op+" CAST($1 AS bigint)") || !reflect.DeepEqual(s.Arguments(), []any{int64(2)}) {
			t.Fatal(s.SQL(), s.Arguments(), e)
		}
	}
	s, e := orm.Compile(expr.Where(m.Query(), expr.Equal(expr.Add(id, v), expr.Multiply(id, expr.Param(orm.KeyExpr(m.ID), 3)))))
	if e != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3)}) || !strings.Contains(s.SQL(), " + ") || !strings.Contains(s.SQL(), " * ") {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	s, e = orm.Compile(expr.Where(m.Query(), expr.Like(expr.Lower(expr.Value(orm.TextExpr(m.Name))), expr.Param(orm.TextExpr(m.Name), "%_!"))))
	if e != nil || !strings.Contains(s.SQL(), " LIKE CAST($1 AS text)") || !reflect.DeepEqual(s.Arguments(), []any{"%_!"}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
}
func TestValueComparisonPhasesAndInvalidOperands(t *testing.T) {
	m := declareMembers()
	base := rows.From(m.Query())
	id := expr.Value(orm.KeyExpr(m.ID))
	count := expr.Count[member]()
	for _, p := range []expr.HavingPredicate[member]{expr.GreaterValue(expr.AsSelected(count), expr.AsSelected(expr.ParamOf(count, 1))), expr.EqualValue(expr.AsSelected(count), expr.AsSelected(count)), expr.LessNullableValue(expr.AsSelected(expr.Min(orm.NullableTextExpr(m.Nickname))), expr.AsSelected(expr.Min(orm.NullableTextExpr(m.Nickname))))} {
		if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(base, expr.AsSelected(count)), p))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(base, expr.AsSelected(expr.Row(id))), expr.GreaterValue(expr.AsSelected(count), expr.AsSelected(expr.ParamOf(count, 1)))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("binary HAVING did not group", e)
	}
	window := expr.RowNumber(expr.WindowFor(rows.ModelScope(m.Query())))
	for _, p := range []expr.HavingPredicate[member]{expr.EqualValue(window, expr.AsSelected(count)), expr.EqualValue(expr.AsSelected(count), window)} {
		if _, e := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(base, expr.AsSelected(count)), p))); !errors.Is(e, fault.Invalid) {
			t.Fatal("window HAVING", e)
		}
	}
	for _, p := range []expr.Predicate[member]{expr.Equal[member, int64](nil, id), expr.Equal[member, int64](id, nil), expr.Equal(id, expr.Value(orm.KeyExpr(declareMembers().ID))), expr.Equal(rows.ScalarRowQuery(base, rows.Scalar[member, int64]{}), expr.NullableRow(id))} {
		if _, e := orm.Compile(expr.Where(m.Query().Limit(0), p)); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid operand", e)
		}
	}
	field := querycore.FieldRef{Table: "members", Column: "id"}
	node := rows.SourceOf(base).Node
	for _, p := range []querycore.Expression{
		querycore.BinaryComparison{Left: field, Right: field, Operator: querycore.Contains}, querycore.BinaryComparison{Left: field, Right: field, Operator: querycore.InsensitiveContains}, querycore.BinaryComparison{Left: field, Right: field, Operator: 255},
		querycore.BinaryComparison{Left: field, Right: querycore.AggregateNode{Kind: querycore.CountAll}, Operator: querycore.Equal}, querycore.BinaryComparison{Right: field, Left: querycore.AggregateNode{Kind: querycore.CountAll}, Operator: querycore.Equal},
	} {
		node.Predicates = []querycore.Expression{p}
		if _, e := querycore.CompileRead(node, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid binary comparison", e)
		}
	}
}
func TestBinaryComparisonRequalificationOwnsStructure(t *testing.T) {
	m := declareMembers()
	id := expr.Value(orm.KeyExpr(m.ID))
	original := expr.FoundryPredicate(expr.Equal(expr.Add(id, expr.Param(orm.KeyExpr(m.ID), 1)), expr.Subtract(id, expr.Param(orm.KeyExpr(m.ID), 2))), sqlowner.Expressions()).Node
	changed := querycore.Requalify(original, "other")
	collect := func(p querycore.Expression) []string {
		var names []string
		w := querycore.SelectWalk{Field: func(f querycore.FieldRef) { names = append(names, f.Table) }}
		w.Expression(p, 0)
		if w.Err != nil {
			t.Fatal(w.Err)
		}
		return names
	}
	if !reflect.DeepEqual(collect(original), []string{"members", "members"}) || !reflect.DeepEqual(collect(changed), []string{"other", "other"}) {
		t.Fatal("requalification mutated operand")
	}
}
