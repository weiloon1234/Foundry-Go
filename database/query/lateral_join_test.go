package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLateralSourceScopeWindowsAndNullability(t *testing.T) {
	a, b := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b")
	c := Correlate(a, b)
	l, r := scopedID(OuterScope(c, a.Scope())), scopedID(InnerScope(c, b.Scope()))
	record := SelectCorrelatedRecord(c.Where(Less(r, l)), InnerScope(c, b.Scope())).OrderBy(r.Desc()).Limit(1)
	latest := AsLateral[thirdAlias](record, "latest")
	j := LeftJoinLateral(a, latest)
	x := scopedID(LeftScope(j, a.Scope()))
	y := nullableScopedID(NullableRightScope(j, latest.Scope()))
	pairs := pairProjection(j, Nullable(Value(x)), Value(y))
	statement, err := pairs.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`LEFT JOIN LATERAL (SELECT "b"."id", "b"."rank" FROM "records" AS "b"`, `"b"."id" < "a"."id"`, `ORDER BY "b"."id" DESC LIMIT $1) AS "latest" ON TRUE`} {
		if !strings.Contains(statement.SQL(), part) {
			t.Fatal(statement.SQL(), part)
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(1)}) {
		t.Fatal(statement.Arguments())
	}
	for _, kind := range []joinKind{innerJoin, crossJoin} {
		node := pairs.source.node
		node.Joins = append([]joinNode(nil), node.Joins...)
		node.Joins[0].Kind = kind
		copy := pairs
		copy.source.node = node
		if _, err := copy.Compile(); err != nil {
			t.Fatal(kind, err)
		}
	}
	// Reusing one correlated record with a new window must leave its original intact.
	_ = AsLateral[thirdAlias](record.Offset(2), "later")
	again, err := pairs.Compile()
	if err != nil || again.SQL() != statement.SQL() {
		t.Fatal("lateral query mutated", err)
	}
}

func TestLateralRejectsMissingOrForwardScopesEvenWithZeroLimit(t *testing.T) {
	a, b := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b")
	c := Correlate(a, b)
	record := SelectCorrelatedRecord(c, InnerScope(c, b.Scope()))
	latest := AsLateral[thirdAlias](record, "latest")
	j := LeftJoinLateral(a, latest)
	x := scopedID(LeftScope(j, a.Scope()))
	valid := SelectValue(j, Value(x))
	for _, name := range []string{"right", "full", "not derived", "self", "forward", "missing", "cross ON"} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			bad.query.source.node.Joins = append([]joinNode(nil), valid.query.source.node.Joins...)
			join := &bad.query.source.node.Joins[0]
			switch name {
			case "right":
				join.Kind = rightJoin
			case "full":
				join.Kind = fullJoin
			case "not derived":
				join.Source.Query = nil
			case "self":
				join.Lateral = &scopeRequirement{Sources: map[string][]columnData{"latest": join.Source.Columns}}
			case "forward":
				join.Lateral = &scopeRequirement{Sources: map[string][]columnData{"future": join.Source.Columns}}
				bad.query.source.node.Joins = append(bad.query.source.node.Joins, joinNode{Kind: crossJoin, Source: tableSource{Table: "records", Alias: "future", Columns: join.Source.Columns}})
			case "missing":
				join.Lateral = &scopeRequirement{}
			case "cross ON":
				join.Kind, join.On = crossJoin, scopedID(a.Scope()).Eq(1).expression
			}
			if _, err := bad.Limit(0).Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
		})
	}
	wrong := LeftJoinLateral(As[firstAlias](cursorQuery(), "different"), latest)
	if _, err := SelectRecord(wrong, LeftScope(wrong, a.Scope())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	badOn := LeftJoinLateral(a, latest, JoinOn[Alias[firstAlias, cursorRecord], Alias[thirdAlias, cursorRecord]]{})
	if _, err := SelectRecord(badOn, LeftScope(badOn, a.Scope())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestLateralCorrelationQualificationPreservesOriginal(t *testing.T) {
	columns := []columnData{{Name: "id"}}
	requirement := &scopeRequirement{Sources: map[string][]columnData{"parent": columns}}
	nested := selectNode{Source: tableSource{Table: "records", Alias: "inner", Columns: columns},
		Predicates: []expression{binaryComparison{Left: fieldRef{Table: "inner", Column: "id"}, Right: fieldRef{Table: "parent", Column: "id"}, Operator: equal}}}
	original := subquery{Correlation: requirement, Node: selectNode{
		Source: tableSource{Table: "records", Alias: "child", Columns: columns},
		Joins:  []joinNode{{Kind: leftJoin, Lateral: requirement, Source: tableSource{Alias: "lateral", Columns: columns, Query: &nested}}},
	}}
	renamed := requalifyCorrelation(original, "qualified")
	if renamed.Err != nil {
		t.Fatal(renamed.Err)
	}
	join := renamed.Node.Joins[0]
	if join.Lateral.Sources["qualified"] == nil || join.Lateral.Sources["parent"] != nil {
		t.Fatal(join.Lateral)
	}
	if got := join.Source.Query.Predicates[0].(binaryComparison).Right.(fieldRef).Table; got != "qualified" {
		t.Fatal(got)
	}
	if got := original.Node.Joins[0].Source.Query.Predicates[0].(binaryComparison).Right.(fieldRef).Table; got != "parent" {
		t.Fatal("original correlation mutated", got)
	}
	if original.Node.Joins[0].Lateral.Sources["parent"] == nil {
		t.Fatal("original requirement mutated")
	}
}
