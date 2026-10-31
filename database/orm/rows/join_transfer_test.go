package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

func TestJoinSourceWindowsAndParameterOrder(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query().Where(orm.NumberKeyGt(orm.NumberKey(m.Key()), 1)).Limit(2), "a")
	b := As[right](m.Query().Where(orm.NumberKeyLt(orm.NumberKey(m.Key()), 9)).OrderBy(orm.KeyDesc(m.ID)).Offset(1), "b")
	x, y := expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))
	j := LeftJoin(a, b, On(x, y).WhereLeft(expr.Ne(x, 5)).WhereRight(expr.Ne(y, 6)))
	id := expr.At(LeftScope(j, Scope(a)), orm.KeyExpr(m.ID))
	q := LimitScalar(OrderScalar(WhereScalar(SelectScalar(j, expr.AsSelected(expr.Row(id))), expr.Ne(id, 7)), expr.Asc(id)), 4)
	s, e := CompileRead(ReadScalar(q))
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{`WHERE ("members"."id" > $1) LIMIT $2) AS "a"`, `WHERE ("members"."id" < $3) ORDER BY "members"."id" DESC OFFSET $4) AS "b"`, `ON ((("a"."id" = "b"."id") AND ("a"."id" <> $5)) AND ("b"."id" <> $6)) WHERE ("a"."id" <> $7) ORDER BY "a"."id" ASC LIMIT $8`} {
		if !strings.Contains(s.SQL(), part) {
			t.Fatal(s.SQL(), part)
		}
	}
	if !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(2), int64(9), int64(1), int64(5), int64(6), int64(7), int64(4)}) {
		t.Fatal(s.Arguments())
	}
	type third struct{}
	c := As[third](m.Query(), "c")
	rightID := expr.AtNullable(NullableRightScope(j, Scope(b)), orm.KeyExpr(m.ID))
	chain := InnerJoin(j, c, On(rightID, expr.At(Scope(c), orm.KeyExpr(m.ID))))
	if _, e := CompileRead(ReadScalar(SelectScalar(chain, expr.AsSelected(expr.Row(expr.AtNullable(LeftNullableScope(chain, NullableRightScope(j, Scope(b))), orm.KeyExpr(m.ID))))))); e != nil {
		t.Fatal(e)
	}
	again, _ := CompileRead(ReadScalar(q))
	if again.SQL() != s.SQL() {
		t.Fatal("join chain mutated source")
	}
}
func TestJoinInvalidAliasesScopesAndDescriptors(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	x, y := expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))
	var missing *Aliased[left, member]
	for _, j := range []InnerJoined[Alias[left, member], Alias[right, member]]{
		InnerJoin(a, b, JoinOn[Alias[left, member], Alias[right, member]]{}), InnerJoin(missing, b, On(x, y)), InnerJoin(a, As[right](m.Query(), "bad.alias"), On(x, y)), InnerJoin(a, As[right](m.Query(), "a"), On(x, y)), InnerJoin(a, As[right](orm.Query[member]{}, "b"), On(x, y)),
	} {
		if _, e := CompileRead(ReadRecord(SelectRecord(j, LeftScope(j, Scope(a))))); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid joined source", e)
		}
	}
	duplicate := As[left](m.Query(), "another")
	if InnerJoin(a, duplicate, On(x, expr.At(Scope(duplicate), orm.KeyExpr(m.ID)))).source.Err == nil {
		t.Fatal("duplicate typed alias")
	}
	j := InnerJoin(a, b, On(x, y))
	wrong := expr.At(LeftScope(j, Scope(duplicate)), orm.KeyExpr(m.ID))
	if _, e := CompileRead(ReadScalar(SelectScalar(j, expr.AsSelected(expr.Row(wrong))))); !errors.Is(e, fault.Invalid) {
		t.Fatal("foreign alias scope", e)
	}
	if _, e := CompileRead(ReadScalar(WhereScalar(SelectScalar(a, expr.AsSelected(expr.Row(x))), expr.Eq(expr.At(Scope(duplicate), orm.KeyExpr(m.ID)), 1)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("foreign alias predicate", e)
	}
}
func TestComputedJoinAndCrossBoundaries(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	x, y := expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))
	j := LeftJoin(a, b, OnAnd(OnLess(expr.Add(x, expr.ParamOf(expr.Row(x), 1)), y), OnNotEqual(x, y)))
	s, e := CompileRead(ReadRecord(SelectRecord(j, LeftScope(j, Scope(a)))))
	if e != nil || !strings.Contains(s.SQL(), ` < "b"."id"`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	cross := CrossJoin(a, b)
	q := SelectRecord(cross, LeftScope(cross, Scope(a)))
	s, e = CompileRead(ReadRecord(q))
	if e != nil || !strings.Contains(s.SQL(), ` CROSS JOIN "members" AS "b"`) || strings.Contains(s.SQL(), " ON ") {
		t.Fatal(s.SQL(), e)
	}
	for _, join := range []querycore.JoinNode{{Source: b.source.Node.Source, Kind: querycore.CrossJoin, On: querycore.BinaryComparison{Left: querycore.FieldRef{Table: "a", Column: "id"}, Right: querycore.FieldRef{Table: "b", Column: "id"}, Operator: querycore.Equal}}, {Source: b.source.Node.Source, Kind: querycore.InnerJoin}, {Source: b.source.Node.Source, Kind: 255}} {
		bad := q
		bad.core.Source.Node.Joins = []querycore.JoinNode{join}
		if _, e := CompileRead(ReadRecord(bad)); !errors.Is(e, fault.Invalid) {
			t.Fatal("malformed join", e)
		}
	}
	type third struct{}
	c := As[third](m.Query(), "c")
	chain := LeftJoin(cross, c, On(expr.At(LeftScope(cross, Scope(a)), orm.KeyExpr(m.ID)), expr.At(Scope(c), orm.KeyExpr(m.ID))))
	if _, e := CompileRead(ReadRecord(SelectRecord(chain, LeftScope(chain, RightScope(cross, Scope(b)))))); e != nil {
		t.Fatal(e)
	}
}
func TestDerivedSelectRejectsCyclesAndOuterReferences(t *testing.T) {
	node := querycore.SelectNode{Source: querycore.TableSource{Alias: "nested", Columns: []querycore.Column{{Name: "id"}}}}
	node.Source.Query = &node
	if _, e := querycore.CompileRead(node, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("derived cycle", e)
	}
	inner := From(declareMembers().Query()).source.Node
	inner.Predicates = []querycore.Expression{querycore.BinaryComparison{Left: querycore.FieldRef{Table: "outer", Column: "id"}, Right: querycore.FieldRef{Table: "members", Column: "id"}, Operator: querycore.Equal}}
	outer := querycore.SelectNode{Source: querycore.TableSource{Alias: "outer", Columns: inner.Source.Columns, Query: &inner}, Selections: querycore.ColumnSelections("outer", inner.Source.Columns), Limit: value.Set(0)}
	if _, e := querycore.CompileRead(outer, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("derived accidental correlation", e)
	}
}
