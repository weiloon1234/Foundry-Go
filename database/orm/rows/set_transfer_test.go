package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"reflect"
	"strings"
	"testing"
)

func TestSetInputAndOutputWindows(t *testing.T) {
	m := declareMembers()
	a := ModelRecord(m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 5)).OrderBy(orm.KeyAsc(m.ID)).Limit(2).Offset(1))
	b := ModelRecord(m.Query().Where(orm.KeyEq(m.ID, 9)))
	base := Union(a, b)
	id := expr.At(base.Scope(), orm.KeyExpr(m.ID))
	q := base.Where(expr.Gt(expr.OrderRow(id), 20)).OrderBy(expr.Desc(id)).Limit(7)
	s, e := CompileRead(ReadSet(q))
	if e != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(5), int64(2), int64(1), int64(9), int64(20), int64(7)}) || !strings.Contains(s.SQL(), `LIMIT $2 OFFSET $3) UNION (`) || !strings.Contains(s.SQL(), `ORDER BY "foundry_set"."id" DESC LIMIT $6`) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	before, _ := CompileRead(ReadSet(base))
	_ = q.Where(expr.Lt(expr.OrderRow(id), 100))
	after, _ := CompileRead(ReadSet(base))
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatal("set mutation")
	}
}
func TestSetOperatorsAndSharedCTEs(t *testing.T) {
	m := declareMembers()
	def := CTE("input_rows", ModelRecord(m.Query())).Materialized()
	for i, q := range []SetQuery[member]{Union(def, def), UnionAll(def, def), Intersect(def, def), IntersectAll(def, def), Except(def, def), ExceptAll(def, def)} {
		s, e := CompileRead(ReadSet(q))
		op := []string{"UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL"}[i]
		if e != nil || strings.Count(s.SQL(), `FROM "members"`) != 1 || !strings.Contains(s.SQL(), ") "+op+" (") {
			t.Fatal(s.SQL(), e)
		}
	}
}
func TestSetRejectsInvalidLayoutsAndBounds(t *testing.T) {
	m := declareMembers()
	base := ModelRecord(m.Query())
	changed := base
	changed.core.Record.Columns = append([]querycore.Column(nil), base.core.Record.Columns...)
	changed.core.Record.Columns[1].Name = "different"
	badop := Union(base, base)
	badop.core.Source.Node.Source.Set.Operator = 255
	for _, q := range []SetQuery[member]{{}, Union[member](nil, base), Union(base, changed), Union(base, base).Limit(-1), badop} {
		if s, e := CompileRead(ReadSet(q)); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid set", e)
		}
	}
	cyclic := Union(base, base)
	set := cyclic.core.Source.Node.Source.Set
	set.Left = cyclic.core.Source.Node
	if _, e := CompileRead(ReadSet(cyclic)); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "resource bound") {
		t.Fatal("cyclic set", e)
	}
	d := recursionTable
	source := From(d.Query())
	wide := WhereScalar(SelectScalar(source, expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(d.Key()))))), recursionWide(ModelScope(d.Query()), querycore.MaxParameters/2+1))
	v := Values(wide)
	if _, e := CompileRead(ReadScalar(wide)); e != nil {
		t.Fatal("half budget", e)
	}
	s, e := CompileRead(ReadValues(UnionAllValues(v, v)))
	if !errors.Is(e, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("set parameter budget", e)
	}
}
