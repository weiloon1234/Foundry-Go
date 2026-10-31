package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestAggregateFilterIsolationAndBindings(t *testing.T) {
	d := declareAggregateRecords()
	f := expr.Value(orm.NumberExpr(d.Exact))
	base := expr.Sum(orm.NumberExpr(d.Exact))
	filtered := expr.Filter(expr.Filter(base, expr.Gt(expr.OrderRow(f), 2)), expr.Or(expr.Lt(expr.OrderRow(f), 8), expr.Eq(f, 10)))
	q := rows.SelectScalar(rows.From(d.Query().Where(orm.NumberNe(d.Exact, 4))), expr.AsSelected(filtered))
	q = rows.OrderScalar(rows.HavingScalar(q, expr.GtValue(filtered, decimal.FromInt64(5))), expr.DescValue(filtered))
	s, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil {
		t.Fatal(err)
	}
	want := `SUM(CAST("aggregate_records"."exact" AS numeric)) FILTER (WHERE ("aggregate_records"."exact" > $1) AND (("aggregate_records"."exact" < $2) OR ("aggregate_records"."exact" = $3)))`
	if !strings.Contains(s.SQL(), want) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(8), int64(10), int64(4), int64(2), int64(8), int64(10), "5", int64(2), int64(8), int64(10)}) {
		t.Fatal(s.SQL(), s.Arguments())
	}
	baseCore, _ := expr.FoundryAggregate(base, sqlowner.Expressions())
	emptyCore, _ := expr.FoundryAggregate(expr.Filter(base), sqlowner.Expressions())
	filteredCore, _ := expr.FoundryAggregate(filtered, sqlowner.Expressions())
	left, _ := expr.FoundryAggregate(expr.Filter(filtered, expr.Eq(f, 6)), sqlowner.Expressions())
	right, _ := expr.FoundryAggregate(expr.Filter(filtered, expr.Eq(f, 7)), sqlowner.Expressions())
	if baseCore.Node.(querycore.AggregateNode).Filter != nil || emptyCore.Node.(querycore.AggregateNode).Filter != nil || len(filteredCore.Node.(querycore.AggregateNode).Filter.Predicates) != 2 || reflect.DeepEqual(left.Node, right.Node) {
		t.Fatal("aggregate derivation mutated its source or sibling")
	}
	count := expr.Filter(expr.Count[aggregateRecord](), expr.Gt(expr.OrderRow(f), 0))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(rows.SelectScalar(rows.From(d.Query()), expr.AsSelected(count)), expr.GtValue(count, 0)))); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateFilterWindowAndValidation(t *testing.T) {
	d := declareAggregateRecords()
	f := expr.Value(orm.NumberExpr(d.Exact))
	source := rows.From(d.Query())
	w := expr.WindowFor(rows.ModelScope(d.Query())).OrderBy(expr.Asc(f)).RowsBetween(expr.Preceding(2), expr.CurrentRow())
	s, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.Over(expr.Filter(expr.Exists[aggregateRecord](), expr.Gt(expr.OrderRow(f), 1)), w))))
	if err != nil || !strings.Contains(s.SQL(), `COUNT(*) FILTER (WHERE ("aggregate_records"."exact" > $1)) OVER (`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2)}) {
		t.Fatal(s.SQL(), err)
	}
	filtered := rows.SelectScalar(source, expr.Over(expr.Filter(expr.Count[aggregateRecord](), expr.Gt(expr.OrderRow(f), 0)), expr.WindowFor(rows.ModelScope(d.Query()))))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.HavingScalar(filtered, expr.GtValue(expr.Count[aggregateRecord](), 0)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped window filter accepted", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.GroupByScalar(filtered, expr.Group(f)))); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]expr.Predicate[aggregateRecord]{"zero": {}, "foreign": expr.Eq(expr.Value(orm.NumberExpr(declareAggregateRecords().Exact)), 0)} {
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.Filter(expr.Count[aggregateRecord](), p))))); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	ps := make([]expr.Predicate[aggregateRecord], querycore.MaxExpressionNodes+1)
	for i := range ps {
		ps[i] = expr.Eq(f, 1)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.Filter(expr.Count[aggregateRecord](), ps...))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded filters accepted", err)
	}
	// Phase-typed public filters cannot contain aggregates or windows. The shared
	// compiler must still reject either node at its heterogeneous AST boundary.
	for _, bad := range []querycore.ValueExpression{querycore.AggregateNode{Kind: querycore.CountAll}, querycore.WindowNode{Kind: querycore.RowNumberWindow}} {
		core, _ := expr.FoundryAggregate(expr.Count[aggregateRecord](), sqlowner.Expressions())
		n := core.Node.(querycore.AggregateNode)
		n.Filter = &querycore.AggregateFilter{Predicates: []querycore.Expression{querycore.Comparison{Operand: bad, Operator: querycore.IsNull}}}
		q := expressionQuery(source, expr.AsSelected(expr.Count[aggregateRecord]()), codec.Signed[int64]())
		rows.QuerySource(&q).Node.Selections[0].Expression = n
		if _, err := querycore.CompileRead(rows.QuerySource(&q).Node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid filter phase accepted", err)
		}
	}
}

func TestAggregateFilterCorrelationOwnership(t *testing.T) {
	d := declareAggregateRecords()
	a, b := rows.As[left](d.Query(), "parent"), rows.As[right](d.Query(), "child")
	link := rows.Correlate(a, b)
	x := expr.At(rows.OuterScope(link, rows.Scope(a)), orm.NumberExpr(d.Exact))
	y := expr.At(rows.InnerScope(link, rows.Scope(b)), orm.NumberExpr(d.Exact))
	type scope = rows.Correlation[rows.Alias[left, aggregateRecord], rows.Alias[right, aggregateRecord]]
	count := expr.Count[scope]()
	for name, aggregate := range map[string]expr.OrderedAggregate[scope, int64]{"outer only": expr.Filter(count, expr.Ne(x, 0)), "local": expr.Filter(count, expr.Ne(y, 0)), "mixed": expr.Filter(count, expr.Equal(y, x)), "local argument": expr.Filter(expr.CountFieldAt(rows.InnerScope(link, rows.Scope(b)), orm.NumberExpr(d.Exact)), expr.Ne(x, 0))} {
		_, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(rows.CorrelatedScalarRowQuery(rows.SelectCorrelatedScalar(link, expr.AsSelected(aggregate)))))))
		if name == "outer only" {
			if !errors.Is(err, fault.Invalid) {
				t.Fatal("implicit aggregate ownership change accepted", err)
			}
		} else if err != nil {
			t.Fatal(name, err)
		}
	}
	type third struct{}
	child := rows.As[third](d.Query(), "nested")
	nested := rows.Correlate(link, child)
	grandparent := expr.At(rows.OuterScope(nested, rows.OuterScope(link, rows.Scope(a))), orm.NumberExpr(d.Exact))
	parent := expr.At(rows.OuterScope(nested, rows.InnerScope(link, rows.Scope(b))), orm.NumberExpr(d.Exact))
	inner := expr.At(rows.InnerScope(nested, rows.Scope(child)), orm.NumberExpr(d.Exact))
	for name, p := range map[string]expr.Predicate[scope]{"outer only": nested.Where(expr.Equal(inner, grandparent)).Exists(), "local": nested.Where(expr.Equal(inner, parent)).Exists()} {
		_, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(rows.CorrelatedScalarRowQuery(rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.Filter(count, p))))))))
		if name == "outer only" && !errors.Is(err, fault.Invalid) {
			t.Fatal("nested outer capture changed ownership", err)
		}
		if name == "local" && err != nil {
			t.Fatal("nested local capture rejected", err)
		}
	}
}
