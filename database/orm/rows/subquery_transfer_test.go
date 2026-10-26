package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

func TestSubqueriesShareScopesAndParameterOrder(t *testing.T) {
	d := declareMembers()
	id := expr.Value(d.ID)
	base := From(d.Query())
	inner := LimitScalar(OrderScalar(WhereScalar(SelectScalar(base, expr.Row(id).Value()), expr.OrderRow(id).Gt(2)), id.Asc()), 3)
	scalar := ScalarQuery(base, inner)
	q := LimitScalar(OrderScalar(WhereScalar(SelectScalar(From(d.Query().Where(d.ID.Ne(9))), scalar), InQuery(id, inner)), scalar.Asc()), 4)
	s, e := CompileRead(ReadScalar(q))
	if e != nil || strings.Count(s.SQL(), `SELECT "members"."id" AS "value" FROM "members" WHERE ("members"."id" > `) != 3 || !strings.Contains(s.SQL(), `("members"."id" IN (SELECT`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(9), int64(2), int64(3), int64(2), int64(3), int64(4)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	before, _ := CompileRead(ReadScalar(inner))
	_, _ = CompileRead(ReadScalar(LimitScalar(WhereScalar(inner, expr.OrderRow(id).Lt(7)), 1)))
	after, _ := CompileRead(ReadScalar(inner))
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatal("inner query mutated")
	}
	p := expr.FoundryPredicate(InQuery(id, inner), sqlowner.Expressions()).Node.(querycore.SubqueryPredicate)
	changed := querycore.Requalify(p, "outer_record").(querycore.SubqueryPredicate)
	if changed.Operand.(querycore.FieldRef).Table != "outer_record" || p.Operand.(querycore.FieldRef).Table != "members" || changed.Query.Node.Source.Table != "members" {
		t.Fatal("qualification crossed SELECT boundary")
	}
}
func TestSubqueryInvalidSourcesAndScalarCycle(t *testing.T) {
	d := declareMembers()
	base := From(d.Query())
	id := expr.Value(d.ID)
	valid := SelectScalar(base, expr.Row(id).Value())
	var missing *Input[member]
	for _, p := range []expr.Predicate[member]{ExistsQuery(base, missing), ExistsQuery(base, Input[member]{}), InQuery(id, Scalar[member, int64]{})} {
		if _, e := CompileRead(ReadScalar(WhereScalar(valid, p))); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid subquery source", e)
		}
	}
	for _, v := range []expr.Selected[member, value.Nullable[int64]]{ScalarQuery(base, Scalar[member, int64]{}), ScalarQuery(missing, valid), ScalarQuery(base, LimitScalar(valid, -1))} {
		if s, e := CompileRead(ReadScalar(SelectScalar(base, v))); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid scalar", e)
		}
	}
	schema := declareRows()
	incomplete := Select(base, schema, Map(schema.ID, id))
	if _, e := CompileRead(ReadScalar(WhereScalar(valid, ExistsQuery(base, incomplete)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("incomplete projection EXISTS", e)
	}
	node := base.source.Node
	nested := &querycore.SelectNode{}
	node.Selections = []querycore.SelectItem{{Expression: querycore.ScalarSubquery{Query: querycore.Subquery{Node: querycore.SelectNode{Source: querycore.TableSource{Alias: "loop", Query: nested, Columns: []querycore.Column{{Name: "id"}}}, Selections: []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "loop", Column: "id"}}}}}}}}
	*nested = node
	if _, e := querycore.CompileRead(node, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "resource bound") {
		t.Fatal("scalar cycle", e)
	}
}
func TestSubqueryScopesAndExistsPreserveSelectLevels(t *testing.T) {
	d := declareMembers()
	base := From(d.Query())
	id := expr.Value(d.ID)
	outer := As[left](d.Query(), "outer_record")
	inner := SelectScalar(base, expr.Row(id).Value())
	inner.source.Node.Predicates = []querycore.Expression{querycore.BinaryComparison{Left: querycore.FieldRef{Table: "members", Column: "id"}, Right: querycore.FieldRef{Table: "outer_record", Column: "id"}, Operator: querycore.Equal}}
	if _, e := CompileRead(ReadScalar(SelectScalar(outer, ScalarQuery(outer, inner)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("accidental correlation", e)
	}
	same := As[left](d.Query(), "members")
	sid := expr.At(Scope(same), d.ID)
	scalar := ScalarQuery(same, LimitScalar(SelectScalar(base, expr.Row(id).Value()), 1))
	s, e := CompileRead(ReadScalar(WhereScalar(SelectScalar(same, scalar), sid.Eq(4))))
	if e != nil || !strings.Contains(s.SQL(), `WHERE ("members"."id" = $2)`) {
		t.Fatal("inner scope leaked", s.SQL(), e)
	}
	count := expr.Count[member]()
	grouped := LimitScalar(HavingScalar(SelectScalar(base, count.Value()), count.Eq(0)), 0)
	s, e = CompileRead(ReadScalar(WhereScalar(SelectScalar(base, expr.Row(id).Value()), expr.Not(ExistsQuery(base, grouped)))))
	if e != nil || !strings.Contains(s.SQL(), `NOT EXISTS(SELECT COUNT(*) AS "value" FROM "members" HAVING (COUNT(*) = $1) LIMIT $2)`) {
		t.Fatal("EXISTS lost grouped/window semantics", s.SQL(), e)
	}
}
