package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestJoinConditionCompositionAndSideFilters(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	ai, bi := expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))
	an, bn := expr.At(Scope(a), orm.TextExpr(m.Name)), expr.At(Scope(b), orm.TextExpr(m.Name))
	base := OnEqual(ai, bi)
	on := OnAnd(base, OnOr(OnLess(expr.Add(ai, ai), expr.Add(bi, bi)), OnNotDistinctFrom(an, bn)).Not()).WhereLeft(an.Ne("left")).WhereRight(bn.Ne("right"))
	j := LeftJoin(a, b, on)
	s := outerSchema()
	q := Select(j, s, MapSelected(s.ID, expr.NullableRow(expr.At(LeftScope(j, Scope(a)), orm.KeyExpr(m.ID))).Value()), Map(s.Nickname, expr.AtNullable(NullableRightScope(j, Scope(b)), orm.TextExpr(m.Name))))
	state := &driverState{columns: []string{"id", "nickname"}}
	if _, err := Read(q).All(t.Context(), openTest(t, state)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.sql, "IS NOT DISTINCT FROM") || !strings.Contains(state.sql, "NOT ") || !strings.Contains(state.sql, `"a"."name" <> $1`) || !strings.Contains(state.sql, `"b"."name" <> $2`) || strings.Contains(state.sql, " WHERE ") {
		t.Fatal(state.sql)
	}
	if _, ok := base.core.Node.(querycore.BinaryComparison); !ok {
		t.Fatal("derived condition mutated parent")
	}
	// Same Go scope/tag with different SQL aliases still fails the side check.
	other := As[left](m.Query(), "outside")
	invalid := InnerJoin(a, b, base.WhereLeft(expr.At(Scope(other), orm.TextExpr(m.Name)).Eq("x")))
	if invalid.source.Err == nil {
		t.Fatal("foreign alias filter accepted")
	}
	if InnerJoin(a, b, OnEqual(expr.RowExpression[Alias[left, member], int64]{}, bi)).source.Err == nil {
		t.Fatal("zero row expression accepted")
	}
}
func TestJoinConditionBoundsAndNullableOperands(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	conditions := make([]JoinOn[Alias[left, member], Alias[right, member]], querycore.MaxExpressionNodes+1)
	if InnerJoin(a, b, OnAnd(conditions...)).source.Err == nil {
		t.Fatal("unbounded ON list")
	}
	on := OnLessNullable(expr.NullableRow(expr.At(Scope(a), orm.KeyExpr(m.ID))), expr.NullableRow(expr.At(Scope(b), orm.KeyExpr(m.ID))))
	if InnerJoin(a, b, on).source.Err != nil {
		t.Fatal("nullable ON rejected")
	}
	leftNull, rightNull := expr.At(Scope(a), orm.NullableTextExpr(m.Nickname)), expr.At(Scope(b), orm.NullableTextExpr(m.Nickname))
	if InnerJoin(a, b, OnLikeNullable(leftNull, rightNull)).source.Err != nil {
		t.Fatal("nullable LIKE rejected")
	}
}
