package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestCTESharedDefinitionsAndBindings(t *testing.T) {
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	definition := CTE("eligible", cursorQuery().Where(id.Eq(2))).Materialized()
	left, right := As[firstAlias](definition, "a"), As[secondAlias](definition, "b")
	x, y := scopedID(left.Scope()), scopedID(right.Scope())
	joined := InnerJoin(left, right, On(x, y))
	field := scopedID(LeftScope(joined, left.Scope()))
	q := SelectValue(joined, field.Value()).Where(field.Ne(9))
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.SQL(), `WITH "eligible" ("id", "rank") AS MATERIALIZED (`) || strings.Count(s.SQL(), `FROM "records"`) != 1 || !strings.Contains(s.SQL(), `FROM "eligible" AS "a" INNER JOIN "eligible" AS "b"`) {
		t.Fatal("CTE definition was duplicated or lost its reference", s.SQL())
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(9)}) {
		t.Fatal("CTE bindings were not before the main SELECT", s.Arguments())
	}
	_ = definition.NotMaterialized()
	after, err := q.Compile()
	if err != nil || after.SQL() != s.SQL() {
		t.Fatal("materialization derivation mutated captured CTE", err)
	}
	if definition.Materialized().definition != definition.definition {
		t.Fatal("unchanged materialization changed definition identity")
	}
}

func TestCTEDependenciesAndValidation(t *testing.T) {
	first := CTE("first_rows", cursorQuery()).NotMaterialized()
	second := CTE("second_rows", first)
	source := As[firstAlias](second, "result")
	s, err := SelectValue(source, scopedID(source.Scope()).Value()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if a, b := strings.Index(s.SQL(), `"first_rows" ("id", "rank") AS NOT MATERIALIZED`), strings.Index(s.SQL(), `"second_rows" ("id", "rank") AS`); a < 0 || b <= a {
		t.Fatal("CTE dependency order changed", s.SQL())
	}
	for _, table := range []CommonTable[cursorRecord]{CommonTable[cursorRecord]{}, CTE[cursorRecord]("empty", nil), CTE("bad.name", cursorQuery()), CTE("records", cursorQuery()), CTE("eager", cursorQuery().With(testRelation(cursorQuery(), cursorQuery())))} {
		a := As[firstAlias](table, "a")
		if s, err := SelectValue(a, scopedID(a.Scope()).Value()).Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid CTE accepted", err)
		}
	}
	a, b := As[firstAlias](first, "a"), As[secondAlias](first.Materialized(), "b")
	joined := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
	if _, err := SelectValue(joined, scopedID(LeftScope(joined, a.Scope())).Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("conflicting CTE definitions accepted", err)
	}
}

func TestCTECyclesAndSharedParameterBound(t *testing.T) {
	d := &cteNode{name: "cycle", columns: []Column{{Name: "id"}}}
	d.query = selectNode{source: tableSource{cte: d, columns: d.columns}, selections: columnSelections("cycle", d.columns)}
	var c compiler
	if _, err := c.compileSelect(d.query); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "cyclic") {
		t.Fatal("CTE dependency cycle accepted", err)
	}
	id := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	base := cursorQuery()
	definition := CTE("wide", base.Where(wideScoped(base.Scope(), MaxParameters/2+1)))
	a := As[firstAlias](definition, "a")
	inner := SelectValue(a, scopedID(a.Scope()).Value())
	if s, err := cursorQuery().Where(id.InQuery(inner), wideScoped(base.Scope(), MaxParameters/2+1)).Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("CTE bypassed shared parameter bound", err)
	}
}
