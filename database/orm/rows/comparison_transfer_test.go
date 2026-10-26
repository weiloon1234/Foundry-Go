package rows

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
	id := expr.Value(m.ID)
	v := expr.Param(m.ID, 2)
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
	s, e := orm.Compile(expr.Where(m.Query(), expr.Equal(expr.Add(id, v), expr.Multiply(id, expr.Param(m.ID, 3)))))
	if e != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3)}) || !strings.Contains(s.SQL(), " + ") || !strings.Contains(s.SQL(), " * ") {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	s, e = orm.Compile(expr.Where(m.Query(), expr.Like(expr.Lower(expr.Value(m.Name)), expr.Param(m.Name, "%_!"))))
	if e != nil || !strings.Contains(s.SQL(), " LIKE CAST($1 AS text)") || !reflect.DeepEqual(s.Arguments(), []any{"%_!"}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
}
func TestValueComparisonPhasesAndInvalidOperands(t *testing.T) {
	m := declareMembers()
	base := From(m.Query())
	id := expr.Value(m.ID)
	count := expr.Count[member]()
	for _, p := range []expr.HavingPredicate[member]{expr.GreaterValue(count.Value(), count.Param(1).Value()), expr.EqualValue(count.Value(), count.Value()), expr.LessNullableValue(expr.Min(m.Nickname).Value(), expr.Min(m.Nickname).Value())} {
		if _, e := CompileRead(ReadScalar(HavingScalar(SelectScalar(base, count.Value()), p))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := CompileRead(ReadScalar(HavingScalar(SelectScalar(base, expr.Row(id).Value()), expr.GreaterValue(count.Value(), count.Param(1).Value())))); !errors.Is(e, fault.Invalid) {
		t.Fatal("binary HAVING did not group", e)
	}
	window := expr.RowNumber(expr.WindowFor(ModelScope(m.Query())))
	for _, p := range []expr.HavingPredicate[member]{expr.EqualValue(window, count.Value()), expr.EqualValue(count.Value(), window)} {
		if _, e := CompileRead(ReadScalar(HavingScalar(SelectScalar(base, count.Value()), p))); !errors.Is(e, fault.Invalid) {
			t.Fatal("window HAVING", e)
		}
	}
	for _, p := range []expr.Predicate[member]{expr.Equal[member, int64](nil, id), expr.Equal[member, int64](id, nil), expr.Equal(id, expr.Value(declareMembers().ID)), expr.Equal(ScalarRowQuery(base, Scalar[member, int64]{}), expr.NullableRow(id))} {
		if _, e := orm.Compile(expr.Where(m.Query().Limit(0), p)); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid operand", e)
		}
	}
	field := querycore.FieldRef{Table: "members", Column: "id"}
	node := base.source.Node
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
	id := expr.Value(m.ID)
	original := expr.FoundryPredicate(expr.Equal(expr.Add(id, expr.Param(m.ID, 1)), expr.Subtract(id, expr.Param(m.ID, 2))), sqlowner.Expressions()).Node
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
