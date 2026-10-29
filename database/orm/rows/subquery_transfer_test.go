package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

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
	id := expr.Value(orm.KeyExpr(d.ID))
	base := rows.From(d.Query())
	inner := rows.LimitScalar(rows.OrderScalar(rows.WhereScalar(rows.SelectScalar(base, expr.Row(id).Value()), expr.OrderRow(id).Gt(2)), id.Asc()), 3)
	scalar := rows.ScalarRowQuery(base, inner).Value()
	q := rows.LimitScalar(rows.OrderScalar(rows.WhereScalar(rows.SelectScalar(rows.From(d.Query().Where(orm.KeyNe(d.ID, 9))), scalar), rows.InQuery(id, inner)), scalar.Asc()), 4)
	s, e := rows.CompileRead(rows.ReadScalar(q))
	if e != nil || strings.Count(s.SQL(), `SELECT "members"."id" AS "value" FROM "members" WHERE ("members"."id" > `) != 3 || !strings.Contains(s.SQL(), `("members"."id" IN (SELECT`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(9), int64(2), int64(3), int64(2), int64(3), int64(4)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	before, _ := rows.CompileRead(rows.ReadScalar(inner))
	_, _ = rows.CompileRead(rows.ReadScalar(rows.LimitScalar(rows.WhereScalar(inner, expr.OrderRow(id).Lt(7)), 1)))
	after, _ := rows.CompileRead(rows.ReadScalar(inner))
	if before.SQL() != after.SQL() || !reflect.DeepEqual(before.Arguments(), after.Arguments()) {
		t.Fatal("inner query mutated")
	}
	p := expr.FoundryPredicate(rows.InQuery(id, inner), sqlowner.Expressions()).Node.(querycore.SubqueryPredicate)
	changed := querycore.Requalify(p, "outer_record").(querycore.SubqueryPredicate)
	if changed.Operand.(querycore.FieldRef).Table != "outer_record" || p.Operand.(querycore.FieldRef).Table != "members" || changed.Query.Node.Source.Table != "members" {
		t.Fatal("qualification crossed SELECT boundary")
	}
}
func TestSubqueryInvalidSourcesAndScalarCycle(t *testing.T) {
	d := declareMembers()
	base := rows.From(d.Query())
	id := expr.Value(orm.KeyExpr(d.ID))
	valid := rows.SelectScalar(base, expr.Row(id).Value())
	var missing *rows.Input[member]
	for _, p := range []expr.Predicate[member]{rows.ExistsQuery(base, missing), rows.ExistsQuery(base, rows.Input[member]{}), rows.InQuery(id, rows.Scalar[member, int64]{})} {
		if _, e := rows.CompileRead(rows.ReadScalar(rows.WhereScalar(valid, p))); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid subquery source", e)
		}
	}
	for _, v := range []expr.Selected[member, value.Nullable[int64]]{rows.ScalarRowQuery(base, rows.Scalar[member, int64]{}).Value(), rows.ScalarRowQuery(missing, valid).Value(), rows.ScalarRowQuery(base, rows.LimitScalar(valid, -1)).Value()} {
		if s, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(base, v))); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid scalar", e)
		}
	}
	schema := declareRows()
	incomplete := rows.Select(base, schema, rows.Map(schema.ID, id))
	if _, e := rows.CompileRead(rows.ReadScalar(rows.WhereScalar(valid, rows.ExistsQuery(base, incomplete)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("incomplete projection EXISTS", e)
	}
	node := rows.SourceOf(base).Node
	nested := &querycore.SelectNode{}
	node.Selections = []querycore.SelectItem{{Expression: querycore.ScalarSubquery{Query: querycore.Subquery{Node: querycore.SelectNode{Source: querycore.TableSource{Alias: "loop", Query: nested, Columns: []querycore.Column{{Name: "id"}}}, Selections: []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "loop", Column: "id"}}}}}}}}
	*nested = node
	if _, e := querycore.CompileRead(node, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "resource bound") {
		t.Fatal("scalar cycle", e)
	}
}
func TestSubqueryScopesAndExistsPreserveSelectLevels(t *testing.T) {
	d := declareMembers()
	base := rows.From(d.Query())
	id := expr.Value(orm.KeyExpr(d.ID))
	outer := rows.As[left](d.Query(), "outer_record")
	inner := rows.SelectScalar(base, expr.Row(id).Value())
	rows.ScalarSource(&inner).Node.Predicates = []querycore.Expression{querycore.BinaryComparison{Left: querycore.FieldRef{Table: "members", Column: "id"}, Right: querycore.FieldRef{Table: "outer_record", Column: "id"}, Operator: querycore.Equal}}
	if _, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(outer, rows.ScalarRowQuery(outer, inner).Value()))); !errors.Is(e, fault.Invalid) {
		t.Fatal("accidental correlation", e)
	}
	same := rows.As[left](d.Query(), "members")
	sid := expr.At(rows.Scope(same), orm.KeyExpr(d.ID))
	scalar := rows.ScalarRowQuery(same, rows.LimitScalar(rows.SelectScalar(base, expr.Row(id).Value()), 1)).Value()
	s, e := rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(same, scalar), sid.Eq(4))))
	if e != nil || !strings.Contains(s.SQL(), `WHERE ("members"."id" = $2)`) {
		t.Fatal("inner scope leaked", s.SQL(), e)
	}
	count := expr.Count[member]()
	grouped := rows.LimitScalar(rows.HavingScalar(rows.SelectScalar(base, count.Value()), count.Eq(0)), 0)
	s, e = rows.CompileRead(rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(base, expr.Row(id).Value()), expr.Not(rows.ExistsQuery(base, grouped)))))
	if e != nil || !strings.Contains(s.SQL(), `NOT EXISTS(SELECT COUNT(*) AS "value" FROM "members" HAVING (COUNT(*) = $1) LIMIT $2)`) {
		t.Fatal("EXISTS lost grouped/window semantics", s.SQL(), e)
	}
}
