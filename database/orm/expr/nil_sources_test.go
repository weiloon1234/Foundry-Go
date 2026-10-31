package expr_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestEmptySelectedSourcesRetainInvalidDescriptors(t *testing.T) {
	type scope struct{}
	seal := sqlowner.Expressions()
	if core := expr.FoundrySelected(expr.AsSelected[scope, int64](nil), seal); core.Err == nil {
		t.Fatal("empty selection source was accepted")
	}
	if core := expr.FoundryRowValue(expr.ParamOf[scope, int64](nil, 0), seal); core.Err == nil {
		t.Fatal("empty parameter source was accepted")
	}
	for _, order := range []expr.SelectedOrder[scope]{expr.AscValue[scope, int64](nil), expr.DescValue[scope, int64](nil)} {
		if core := expr.FoundryProjectionOrder(order, seal); core.Err == nil {
			t.Fatal("empty selected order source was accepted")
		}
	}
	for _, predicate := range []expr.HavingPredicate[scope]{
		expr.ValueFilterAdapter[scope, int64]{}.Eq(1),
		expr.NullableValueFilterAdapter[scope, int64]{}.IsNull(),
		expr.OrderValueFilterAdapter[scope, int64]{}.Gt(1),
		expr.NullableOrderValueFilterAdapter[scope, int64]{}.IsNotNull(),
		expr.TextValueFilterAdapter[scope, string]{}.Contains("x"),
		expr.NullableTextValueFilterAdapter[scope, string]{}.Like("x%"),
	} {
		if core := expr.FoundryHaving(predicate, seal); core.Err == nil {
			t.Fatal("empty filter adapter was accepted")
		}
	}
}
