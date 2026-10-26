package rows

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestCTESharedDefinitionsAndBindings(t *testing.T) {
	base := recursionSource()
	id := recursionID(ModelScope(recursionTable.Query()))
	definition := CTE("eligible", base.Where(id.Eq(2))).Materialized()
	a, b := AsRecord[left](definition, "a"), AsRecord[right](definition, "b")
	j := InnerJoin(a, b, On(recursionID(Scope(a)), recursionID(Scope(b))))
	field := recursionID(LeftScope(j, Scope(a)))
	q := WhereScalar(SelectScalar(j, expr.Row(field).Value()), field.Ne(9))
	s, err := CompileRead(ReadScalar(q))
	if err != nil || !strings.HasPrefix(s.SQL(), `WITH "eligible" ("id", "rank") AS MATERIALIZED (`) || strings.Count(s.SQL(), `FROM "records"`) != 1 || !strings.Contains(s.SQL(), `FROM "eligible" AS "a" INNER JOIN "eligible" AS "b"`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(9)}) {
		t.Fatal(s.SQL(), s.Arguments(), err)
	}
	_ = definition.NotMaterialized()
	after, err := CompileRead(ReadScalar(q))
	if err != nil || after.SQL() != s.SQL() {
		t.Fatal("materialization mutated captured definition", err)
	}
	if definition.Materialized().core.Definition != definition.core.Definition {
		t.Fatal("unchanged materialization changed identity")
	}
}
func TestCTECyclesAndSharedParameterBound(t *testing.T) {
	d := &querycore.CteNode{Name: "cycle", Columns: []querycore.Column{{Name: "id"}}}
	d.Query = querycore.SelectNode{Source: querycore.TableSource{Cte: d, Columns: d.Columns}, Selections: querycore.ColumnSelections("cycle", d.Columns)}
	var c querycore.Compiler
	if _, err := c.CompileSelect(d.Query); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "cyclic") {
		t.Fatal("dependency cycle accepted", err)
	}
	scope := ModelScope(recursionTable.Query())
	base := recursionSource()
	definition := CTE("wide", base.Where(recursionWide(scope, querycore.MaxParameters/2+1)))
	a := AsRecord[left](definition, "a")
	inner := SelectScalar(a, expr.Row(recursionID(Scope(a))).Value())
	q := base.Where(InQuery(recursionID(scope), inner), recursionWide(scope, querycore.MaxParameters/2+1))
	if s, err := CompileRead(q); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("CTE bypassed shared parameter budget", err)
	}
}

func TestCTEDependenciesAndValidation(t *testing.T) {
	first := CTE("first_rows", recursionSource()).NotMaterialized()
	second := CTE("second_rows", first)
	a := AsRecord[left](second, "result")
	q := ReadScalar(SelectScalar(a, expr.Row(recursionID(Scope(a))).Value()))
	s, err := CompileRead(q)
	if err != nil {
		t.Fatal(err)
	}
	if x, y := strings.Index(s.SQL(), `"first_rows" ("id", "rank") AS NOT MATERIALIZED`), strings.Index(s.SQL(), `"second_rows" ("id", "rank") AS`); x < 0 || y <= x {
		t.Fatal(s.SQL())
	}
	for _, cte := range []CommonTable[recursionRecord]{{}, CTE[recursionRecord]("empty", nil), CTE("bad.name", recursionSource()), CTE("records", recursionSource())} {
		alias := AsRecord[left](cte, "a")
		s, e := CompileRead(ReadScalar(SelectScalar(alias, expr.Row(recursionID(Scope(alias))).Value())))
		if !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid CTE", s, e)
		}
	}
	l, r := AsRecord[left](first, "a"), AsRecord[right](first.Materialized(), "b")
	j := InnerJoin(l, r, On(recursionID(Scope(l)), recursionID(Scope(r))))
	if _, e := CompileRead(ReadScalar(SelectScalar(j, expr.Row(recursionID(LeftScope(j, Scope(l)))).Value()))); !errors.Is(e, fault.Invalid) {
		t.Fatal("conflicting definitions", e)
	}
	type model struct {
		ID       int64
		Children relation.Many[model]
	}
	type table struct {
		orm.Table[model, int64]
		Children orm.Many[model, model]
	}
	d := orm.Define("cte_parents", func(b orm.Builder[model], m *model) table {
		return table{orm.TableOf(b, &m.ID), orm.ManyOf(b, &m.Children)}
	})
	eager := ModelRecord(d.Query().With(orm.HasManyOf(d.Children, d.Key(), d.Key())))
	alias := AsRecord[left](CTE("eager", eager), "a")
	if _, e := CompileRead(ReadRecord(SelectRecord(alias, Scope(alias)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("eager load silently lost inside CTE", e)
	}
}
