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
	pairs := pairProjection(j, Nullable(x.Value()), y.Value())
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
		node.joins = append([]joinNode(nil), node.joins...)
		node.joins[0].kind = kind
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
	valid := SelectValue(j, x.Value())
	for _, name := range []string{"right", "full", "not derived", "self", "forward", "missing", "cross ON"} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			bad.query.source.node.joins = append([]joinNode(nil), valid.query.source.node.joins...)
			join := &bad.query.source.node.joins[0]
			switch name {
			case "right":
				join.kind = rightJoin
			case "full":
				join.kind = fullJoin
			case "not derived":
				join.source.query = nil
			case "self":
				join.lateral = &scopeRequirement{sources: map[string][]Column{"latest": join.source.columns}}
			case "forward":
				join.lateral = &scopeRequirement{sources: map[string][]Column{"future": join.source.columns}}
				bad.query.source.node.joins = append(bad.query.source.node.joins, joinNode{kind: crossJoin, source: tableSource{table: "records", alias: "future", columns: join.source.columns}})
			case "missing":
				join.lateral = &scopeRequirement{}
			case "cross ON":
				join.kind, join.on = crossJoin, scopedID(a.Scope()).Eq(1).expression
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
	columns := []Column{{Name: "id"}}
	requirement := &scopeRequirement{sources: map[string][]Column{"parent": columns}}
	nested := selectNode{source: tableSource{table: "records", alias: "inner", columns: columns},
		predicates: []expression{binaryComparison{fieldRef{"inner", "id"}, fieldRef{"parent", "id"}, equal}}}
	original := subquery{correlation: requirement, node: selectNode{
		source: tableSource{table: "records", alias: "child", columns: columns},
		joins:  []joinNode{{kind: leftJoin, lateral: requirement, source: tableSource{alias: "lateral", columns: columns, query: &nested}}},
	}}
	renamed := requalifyCorrelation(original, "qualified")
	if renamed.err != nil {
		t.Fatal(renamed.err)
	}
	join := renamed.node.joins[0]
	if join.lateral.sources["qualified"] == nil || join.lateral.sources["parent"] != nil {
		t.Fatal(join.lateral)
	}
	if got := join.source.query.predicates[0].(binaryComparison).right.(fieldRef).table; got != "qualified" {
		t.Fatal(got)
	}
	if got := original.node.joins[0].source.query.predicates[0].(binaryComparison).right.(fieldRef).table; got != "parent" {
		t.Fatal("original correlation mutated", got)
	}
	if original.node.joins[0].lateral.sources["parent"] == nil {
		t.Fatal("original requirement mutated")
	}
}
