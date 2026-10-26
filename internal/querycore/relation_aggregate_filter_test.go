package querycore

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestAggregateFilterRelationQualificationAndCTEDiscovery(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "rank", Nullable: true}}
	model := ModelDefinition{Table: "records", Primary: "id", Columns: columns}
	q := QueryCore{Table: "records", Model: &model}
	id := FieldRef{Table: "records", Column: "id"}
	eligible := q.ModelSelect(ColumnSelections("records", columns))
	eligible.Predicates = []Expression{BinaryComparison{Left: id, Operator: Greater, Right: ParameterNode{Kind: codec.TypeInteger, Value: RetainLiteral(int64(2))}}}
	cte := &CteNode{Name: "eligible", Columns: columns, Query: eligible}
	filter := SubqueryPredicate{Operand: id, Query: Subquery{Node: SelectNode{Source: TableSource{Cte: cte, Alias: "eligible_source", Columns: columns}, Selections: []SelectItem{{Expression: FieldRef{Table: "eligible_source", Column: "id"}}}}}}
	measure := AggregateNode{Kind: SumValues, Cast: ExactNumberCast, Field: id, Filter: &AggregateFilter{Predicates: []Expression{filter}}}
	joined := RelationThroughSelect(q, q, id, id, nil, RelationTargetAlias, RelationPivotAlias)
	group := FieldRef{Table: RelationPivotAlias, Column: "id"}
	selected, err := RelationAggregateSelect(joined, "records", RelationTargetAlias, group, measure, false, group, []driver.Value{int64(1)}, 9)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := CompileRead(selected, t.Context(), false, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`WITH "eligible" ("id", "rank") AS`, `SUM(CAST("foundry_target"."id" AS numeric)) FILTER (WHERE ("foundry_target"."id" IN (SELECT "eligible_source"."id"`, `COUNT(*), COUNT(DISTINCT "foundry_pivot"."id")`} {
		if !strings.Contains(statement.SQLText, want) {
			t.Fatal("filter lost CTE, target alias or cardinality", statement.SQLText)
		}
	}
	if !reflect.DeepEqual(statement.Bindings, []any{int64(2), int64(1), int64(9)}) {
		t.Fatal(statement.Bindings)
	}
	bad := measure
	bad.Filter = &AggregateFilter{Predicates: []Expression{BinaryComparison{Left: FieldRef{Table: "wrong", Column: "id"}, Right: ParameterNode{Kind: codec.TypeInteger, Value: RetainLiteral(int64(1))}, Operator: Equal}}}
	if _, err := RelationAggregateSelect(joined, "records", RelationTargetAlias, group, bad, false, group, nil, 9); !errors.Is(err, fault.Invalid) {
		t.Fatal("requalification concealed wrong owner", err)
	}
	if measure.Field != id || !reflect.DeepEqual(measure.Filter.Predicates, []Expression{filter}) || len(joined.Selections) != 4 || len(joined.GroupBy) != 0 {
		t.Fatal("aggregate lowering mutated its inputs")
	}
}

func TestTypedThroughAggregateSQL(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "rank", Nullable: true}}
	model := ModelDefinition{Table: "records", Primary: "id", Columns: columns}
	q := QueryCore{Table: "records", Model: &model}
	id := FieldRef{Table: "records", Column: "id"}
	predicate := func(op Operator, n int64) Expression {
		return Comparison{Operand: id, Operator: op, Values: RetainComparisonValues([]any{n}), Bind: func(v any) (driver.Value, error) { return v, nil }}
	}
	target, pivot := q, q
	target.Predicates = []Expression{predicate(Greater, 2)}
	pivot.Predicates = []Expression{predicate(Less, 20)}
	target.Orders = []QueryOrder{{Field: id, Descending: true}}
	joined := RelationThroughSelect(target, pivot, id, id, nil, RelationTargetAlias, RelationPivotAlias)
	group := FieldRef{Table: RelationPivotAlias, Column: "id"}
	selected, err := RelationAggregateSelect(joined, "records", RelationTargetAlias, group, AggregateNode{Kind: AverageValues, Cast: ExactNumberCast, Field: id}, false, group, []driver.Value{int64(3), int64(4)}, 9)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := CompileRead(selected, t.Context(), false, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`SELECT "foundry_pivot"."id", AVG(CAST("foundry_target"."id" AS numeric)), COUNT(*), COUNT(DISTINCT "foundry_pivot"."id")`, `WHERE ("foundry_target"."id" > $1) AND ("foundry_pivot"."id" < $2) AND ("foundry_pivot"."id" IN ($3, $4)) GROUP BY "foundry_pivot"."id" LIMIT $5`} {
		if !strings.Contains(statement.SQLText, text) {
			t.Fatal(statement.SQLText)
		}
	}
	if strings.Contains(statement.SQLText, "ORDER BY") || !reflect.DeepEqual(statement.Bindings, []any{int64(2), int64(20), int64(3), int64(4), int64(9)}) {
		t.Fatal(statement)
	}
}
