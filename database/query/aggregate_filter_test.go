package query

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestAggregateFilterIsolationAndBindings(t *testing.T) {
	f := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	base := f.Sum()
	filtered := base.Filter(f.Gt(2)).Filter(Or(f.Lt(8), f.Eq(10)))
	q := SelectValue(cursorQuery().Where(f.Ne(4)), filtered.Value()).Having(filtered.Gt(decimal.FromInt64(5))).OrderBy(filtered.Desc())
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `SUM(CAST("records"."id" AS numeric)) FILTER (WHERE ("records"."id" > $1) AND (("records"."id" < $2) OR ("records"."id" = $3)))`
	if !strings.Contains(s.SQL(), want) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(8), int64(10), int64(4), int64(2), int64(8), int64(10), "5", int64(2), int64(8), int64(10)}) {
		t.Fatal("aggregate-local filters lost placement/bindings", s.SQL(), s.Arguments())
	}
	if base.node.filter != nil || base.Filter().node.filter != nil || len(filtered.node.filter.predicates) != 2 {
		t.Fatal("Filter mutated its source")
	}
	left, right := filtered.Filter(f.Eq(6)), filtered.Filter(f.Eq(7))
	if reflect.DeepEqual(left.node.filter, right.node.filter) || len(filtered.node.filter.predicates) != 2 {
		t.Fatal("derived filters share mutable storage")
	}
	count := Count[cursorRecord]().Filter(f.Gt(0))
	if _, err := SelectValue(cursorQuery(), count.Value()).Having(count.Gt(0)).Compile(); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateFilterWindowAndValidation(t *testing.T) {
	f := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	q := cursorQuery()
	w := WindowFor(q).OrderBy(f.Asc()).RowsBetween(Preceding(2), CurrentRow())
	s, err := SelectValue(q, Exists[cursorRecord]().Filter(f.Gt(1)).Over(w)).Compile()
	if err != nil || !strings.Contains(s.SQL(), `COUNT(*) FILTER (WHERE ("records"."id" > $1)) OVER (`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2)}) {
		t.Fatal("FILTER/OVER order", s.SQL(), err)
	}
	filteredWindow := Count[cursorRecord]().Filter(f.Gt(0)).Over(WindowFor(q))
	if _, err := SelectValue(q, filteredWindow).Having(Count[cursorRecord]().Gt(0)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("ungrouped window filter accepted", err)
	}
	if _, err := SelectValue(q, filteredWindow).GroupBy(f.Group()).Compile(); err != nil {
		t.Fatal(err)
	}
	foreign := NewExactField[cursorRecord, int64]("foreign", "id", codec.Signed[int64]())
	for name, bad := range map[string]Predicate[cursorRecord]{
		"zero": {}, "foreign": foreign.Gt(0),
		"aggregate": {expression: Count[cursorRecord]().Gt(0).expression},
		"window":    {expression: comparison{operand: RowNumber(w).node, operator: isNull}},
	} {
		if _, err := SelectValue(q, Count[cursorRecord]().Filter(bad).Value()).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	predicates := make([]Predicate[cursorRecord], MaxExpressionNodes+1)
	for i := range predicates {
		predicates[i] = f.Eq(1)
	}
	if _, err := SelectValue(q, Count[cursorRecord]().Filter(predicates...).Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded filters accepted", err)
	}
}

func TestAggregateFilterCorrelationOwnership(t *testing.T) {
	a, b := As[firstAlias](cursorQuery(), "parent"), As[secondAlias](cursorQuery(), "child")
	link := Correlate(a, b)
	x, y := scopedID(OuterScope(link, a.Scope())), scopedID(InnerScope(link, b.Scope()))
	count := Count[Correlation[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]]]()
	for name, aggregate := range map[string]OrderedAggregate[Correlation[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]], int64]{
		"outer only":     count.Filter(x.Ne(0)),
		"local":          count.Filter(y.Ne(0)),
		"mixed":          count.Filter(y.EqColumn(x)),
		"local argument": y.Count().Filter(x.Ne(0)),
	} {
		_, err := SelectValue(a, CorrelatedScalarQuery(SelectCorrelatedValue(link, aggregate.Value()))).Compile()
		if name == "outer only" {
			if !errors.Is(err, fault.Invalid) {
				t.Fatal("implicit aggregate ownership change accepted", err)
			}
		} else if err != nil {
			t.Fatal(name, err)
		}
	}
	child := As[thirdAlias](cursorQuery(), "nested")
	nested := Correlate(link, child)
	grandparent := scopedID(OuterScope(nested, OuterScope(link, a.Scope())))
	parent := scopedID(OuterScope(nested, InnerScope(link, b.Scope())))
	inner := scopedID(InnerScope(nested, child.Scope()))
	for name, predicate := range map[string]Predicate[Correlation[Alias[firstAlias, cursorRecord], Alias[secondAlias, cursorRecord]]]{
		"outer only": nested.Where(inner.EqColumn(grandparent)).Exists(),
		"local":      nested.Where(inner.EqColumn(parent)).Exists(),
	} {
		_, err := SelectValue(a, CorrelatedScalarQuery(SelectCorrelatedValue(link, count.Filter(predicate).Value()))).Compile()
		if name == "outer only" && !errors.Is(err, fault.Invalid) {
			t.Fatal("nested outer capture changed ownership", err)
		}
		if name == "local" && err != nil {
			t.Fatal("nested local capture rejected", err)
		}
	}
}

func TestAggregateFilterRelationQualificationAndCTEDiscovery(t *testing.T) {
	f := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	common := As[firstAlias](CTE("eligible", cursorQuery().Where(f.Gt(2))), "eligible_source")
	eligible := scopedID(common.Scope())
	filtered := f.Sum().Filter(f.InQuery(SelectValue(common, eligible.Value())))
	statement, err := compileRelationAggregate(testThrough().aggregateInput(), filtered.node, []driver.Value{int64(1)}, 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`WITH "eligible" ("id", "rank") AS`, `SUM(CAST("foundry_target"."id" AS numeric)) FILTER (WHERE ("foundry_target"."id" IN (SELECT "eligible_source"."id"`, `COUNT(*), COUNT(DISTINCT "foundry_pivot"."id")`} {
		if !strings.Contains(statement.SQL(), want) {
			t.Fatal("filter lost CTE/alias or changed cardinality count", statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(2), int64(1), int64(9)}) {
		t.Fatal(statement.Arguments())
	}
	bad := NewExactField[cursorRecord, int64]("wrong", "id", codec.Signed[int64]())
	if _, err := compileRelationAggregate(testThrough().aggregateInput(), Count[cursorRecord]().Filter(bad.Eq(1)).node, nil, 9); !errors.Is(err, fault.Invalid) {
		t.Fatal("relation requalification concealed wrong owner", err)
	}
	if filtered.node.field.table != "records" {
		t.Fatal("relation compilation mutated aggregate")
	}
}
