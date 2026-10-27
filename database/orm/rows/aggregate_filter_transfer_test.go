package rows

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
	f := expr.Value(d.Exact)
	base := expr.Sum(d.Exact)
	filtered := base.Filter(expr.OrderRow(f).Gt(2)).Filter(expr.Or(expr.OrderRow(f).Lt(8), f.Eq(10)))
	q := SelectScalar(From(d.Query().Where(d.Exact.Ne(4))), filtered.Value())
	q = OrderScalar(HavingScalar(q, filtered.Gt(decimal.FromInt64(5))), filtered.Desc())
	s, err := CompileRead(ReadScalar(q))
	if err != nil {
		t.Fatal(err)
	}
	want := `SUM(CAST("aggregate_records"."exact" AS numeric)) FILTER (WHERE ("aggregate_records"."exact" > $1) AND (("aggregate_records"."exact" < $2) OR ("aggregate_records"."exact" = $3)))`
	if !strings.Contains(s.SQL(), want) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(8), int64(10), int64(4), int64(2), int64(8), int64(10), "5", int64(2), int64(8), int64(10)}) {
		t.Fatal(s.SQL(), s.Arguments())
	}
	baseCore, _ := expr.FoundryAggregate(base, sqlowner.Expressions())
	emptyCore, _ := expr.FoundryAggregate(base.Filter(), sqlowner.Expressions())
	filteredCore, _ := expr.FoundryAggregate(filtered, sqlowner.Expressions())
	left, _ := expr.FoundryAggregate(filtered.Filter(f.Eq(6)), sqlowner.Expressions())
	right, _ := expr.FoundryAggregate(filtered.Filter(f.Eq(7)), sqlowner.Expressions())
	if baseCore.Node.(querycore.AggregateNode).Filter != nil || emptyCore.Node.(querycore.AggregateNode).Filter != nil || len(filteredCore.Node.(querycore.AggregateNode).Filter.Predicates) != 2 || reflect.DeepEqual(left.Node, right.Node) {
		t.Fatal("aggregate derivation mutated its source or sibling")
	}
	count := expr.Count[aggregateRecord]().Filter(expr.OrderRow(f).Gt(0))
	if _, err := CompileRead(ReadScalar(HavingScalar(SelectScalar(From(d.Query()), count.Value()), count.Gt(0)))); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateFilterWindowAndValidation(t *testing.T) {
	d := declareAggregateRecords()
	f := expr.Value(d.Exact)
	source := From(d.Query())
	w := expr.WindowFor(ModelScope(d.Query())).OrderBy(f.Asc()).RowsBetween(expr.Preceding(2), expr.CurrentRow())
	s, err := CompileRead(ReadScalar(SelectScalar(source, expr.Over(expr.Exists[aggregateRecord]().Filter(expr.OrderRow(f).Gt(1)), w))))
	if err != nil || !strings.Contains(s.SQL(), `COUNT(*) FILTER (WHERE ("aggregate_records"."exact" > $1)) OVER (`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2)}) {
		t.Fatal(s.SQL(), err)
	}
	filtered := SelectScalar(source, expr.Over(expr.Count[aggregateRecord]().Filter(expr.OrderRow(f).Gt(0)), expr.WindowFor(ModelScope(d.Query()))))
	if _, err := CompileRead(ReadScalar(HavingScalar(filtered, expr.Count[aggregateRecord]().Gt(0)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped window filter accepted", err)
	}
	if _, err := CompileRead(ReadScalar(GroupByScalar(filtered, expr.Group(f)))); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]expr.Predicate[aggregateRecord]{"zero": {}, "foreign": expr.Value(declareAggregateRecords().Exact).Eq(0)} {
		if _, err := CompileRead(ReadScalar(SelectScalar(source, expr.Count[aggregateRecord]().Filter(p).Value()))); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	ps := make([]expr.Predicate[aggregateRecord], querycore.MaxExpressionNodes+1)
	for i := range ps {
		ps[i] = f.Eq(1)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(source, expr.Count[aggregateRecord]().Filter(ps...).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded filters accepted", err)
	}
	// Phase-typed public filters cannot contain aggregates or windows. The shared
	// compiler must still reject either node at its heterogeneous AST boundary.
	for _, bad := range []querycore.ValueExpression{querycore.AggregateNode{Kind: querycore.CountAll}, querycore.WindowNode{Kind: querycore.RowNumberWindow}} {
		core, _ := expr.FoundryAggregate(expr.Count[aggregateRecord](), sqlowner.Expressions())
		n := core.Node.(querycore.AggregateNode)
		n.Filter = &querycore.AggregateFilter{Predicates: []querycore.Expression{querycore.Comparison{Operand: bad, Operator: querycore.IsNull}}}
		q := expressionQuery(source, expr.Count[aggregateRecord]().Value(), codec.Signed[int64]())
		q.source.Node.Selections[0].Expression = n
		if _, err := querycore.CompileRead(q.source.Node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid filter phase accepted", err)
		}
	}
}

func TestAggregateFilterCorrelationOwnership(t *testing.T) {
	d := declareAggregateRecords()
	a, b := As[left](d.Query(), "parent"), As[right](d.Query(), "child")
	link := Correlate(a, b)
	x := expr.At(OuterScope(link, Scope(a)), d.Exact)
	y := expr.At(InnerScope(link, Scope(b)), d.Exact)
	type scope = Correlation[Alias[left, aggregateRecord], Alias[right, aggregateRecord]]
	count := expr.Count[scope]()
	for name, aggregate := range map[string]expr.OrderedAggregate[scope, int64]{"outer only": count.Filter(x.Ne(0)), "local": count.Filter(y.Ne(0)), "mixed": count.Filter(expr.Equal(y, x)), "local argument": expr.CountFieldAt(InnerScope(link, Scope(b)), d.Exact).Filter(x.Ne(0))} {
		_, err := CompileRead(ReadScalar(SelectScalar(a, CorrelatedScalarRowQuery(SelectCorrelatedScalar(link, aggregate.Value())).Value())))
		if name == "outer only" {
			if !errors.Is(err, fault.Invalid) {
				t.Fatal("implicit aggregate ownership change accepted", err)
			}
		} else if err != nil {
			t.Fatal(name, err)
		}
	}
	type third struct{}
	child := As[third](d.Query(), "nested")
	nested := Correlate(link, child)
	grandparent := expr.At(OuterScope(nested, OuterScope(link, Scope(a))), d.Exact)
	parent := expr.At(OuterScope(nested, InnerScope(link, Scope(b))), d.Exact)
	inner := expr.At(InnerScope(nested, Scope(child)), d.Exact)
	for name, p := range map[string]expr.Predicate[scope]{"outer only": nested.Where(expr.Equal(inner, grandparent)).Exists(), "local": nested.Where(expr.Equal(inner, parent)).Exists()} {
		_, err := CompileRead(ReadScalar(SelectScalar(a, CorrelatedScalarRowQuery(SelectCorrelatedScalar(link, count.Filter(p).Value())).Value())))
		if name == "outer only" && !errors.Is(err, fault.Invalid) {
			t.Fatal("nested outer capture changed ownership", err)
		}
		if name == "local" && err != nil {
			t.Fatal("nested local capture rejected", err)
		}
	}
}
