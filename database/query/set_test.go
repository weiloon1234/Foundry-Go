package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSetInputAndOutputWindows(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	left := cursorQuery().Where(id.Gt(5)).OrderBy(id.Asc()).Limit(2).Offset(1)
	right := cursorQuery().Where(id.Eq(9))
	combined := left.Union(right)
	f := NewOrderedField[Set[cursorRecord], int64](combined.Scope().Table(), "id", codec.Signed[int64]())
	result := combined.Where(f.Gt(20)).OrderBy(f.Desc()).Limit(7)
	s, err := result.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(5), int64(2), int64(1), int64(9), int64(20), int64(7)}) {
		t.Fatal("set parameter order changed", s.Arguments())
	}
	if !strings.Contains(s.SQL(), `LIMIT $2 OFFSET $3) UNION (`) || !strings.Contains(s.SQL(), `ORDER BY "foundry_set"."id" DESC LIMIT $6`) {
		t.Fatal("set windows moved across the boundary", s.SQL())
	}
	before, _ := combined.Compile()
	_ = result.Where(f.Lt(100))
	after, _ := combined.Compile()
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatal("set derivation mutated input")
	}
}

func TestSetOperatorsAndSharedCTEs(t *testing.T) {
	definition := CTE("input_rows", cursorQuery()).Materialized()
	for _, test := range []struct {
		operator string
		query    SetQuery[cursorRecord]
	}{
		{"UNION", Union(definition, definition)}, {"UNION ALL", UnionAll(definition, definition)},
		{"INTERSECT", Intersect(definition, definition)}, {"INTERSECT ALL", IntersectAll(definition, definition)},
		{"EXCEPT", Except(definition, definition)}, {"EXCEPT ALL", ExceptAll(definition, definition)},
	} {
		s, err := test.query.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(s.SQL(), `FROM "records"`) != 1 || !strings.Contains(s.SQL(), ") "+test.operator+" (") {
			t.Fatal("set operation lost shared CTE or operator", s.SQL())
		}
	}
}

func TestSetRejectsInvalidLayoutsAndBounds(t *testing.T) {
	base := cursorQuery()
	changed := *base.definition
	changed.columns = []Column{{Name: "id"}, {Name: "different", Nullable: true}}
	for _, q := range []SetQuery[cursorRecord]{
		{}, Union[cursorRecord](nil, base), base.Union(ForModel(changed)),
		base.Union(base.With(testRelation(base, base))), base.Union(base).Limit(-1),
	} {
		if s, err := q.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid set query accepted", err)
		}
	}
	cycle := &setNode{}
	node := selectNode{source: tableSource{set: cycle, alias: setAlias, columns: base.definition.columns}, selections: columnSelections(setAlias, base.definition.columns)}
	cycle.left, cycle.right = node, base.modelSelect()
	var c compiler
	if _, err := c.compileSelect(node); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "resource bound") {
		t.Fatal("cyclic set AST escaped resource bounds", err)
	}
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	wide := base.Where(id.In(make([]int64, MaxParameters/2+1)...))
	if s, err := wide.UnionAll(wide).Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("set bypassed shared parameter bound", err)
	}
	bad := base.Union(base)
	bad.record.node.source.set.operator = setOperator(255)
	if _, err := bad.Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid set operator compiled", err)
	}
}
