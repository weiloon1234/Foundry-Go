package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type expressionSide struct{}
type expressionResult struct {
	Number  int64
	Exact   decimal.Decimal
	Approx  float64
	Missing value.Nullable[int64]
}
type expressionDeclaration struct {
	rowset.Row[expressionResult]
	Number  rowset.Field[expressionResult, int64]
	Exact   rowset.Field[expressionResult, decimal.Decimal]
	Approx  rowset.Field[expressionResult, float64]
	Missing rowset.Field[expressionResult, value.Nullable[int64]]
}

var expressionRows = rowset.Define(func(b rowset.Builder[expressionResult], r *expressionResult) expressionDeclaration {
	return expressionDeclaration{b.Row(), rowset.NumberOf(b, &r.Number), rowset.NumberOf(b, &r.Exact), rowset.NumberOf(b, &r.Approx), rowset.NullableNumberOf(b, &r.Missing)}
})

func TestStage6a1ExpressionGolden(t *testing.T) {
	after := &goldenCapture{}
	for _, c := range []*goldenCapture{after} {
		c.scriptedColumns = []string{"number", "exact", "approx", "missing"}
		c.scriptedBatches = [][][]driver.Value{{{int64(3), "3", float64(3), nil}}}
	}

	runExpressionNew(after)
	if after.statements != 1 || !oracleEqual(t, "expression_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func runExpressionNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[expressionSide](models.Orders.Query(), "o")
	f := expr.At(rowset.Scope(a), orm.NumberExpr(models.Orders.TotalCents))
	x := expr.Add(f, f)
	result := expr.Divide(x, expr.ParamOf(x, 2))
	s := expressionRows
	q := rowset.OrderBy(rowset.Select(a, s, rowset.MapSelected(s.Number, expr.AsSelected(result)), rowset.MapSelected(s.Exact, expr.AsSelected(expr.DecimalOf(result))), rowset.MapSelected(s.Approx, expr.AsSelected(expr.FloatOf(result))), rowset.MapSelected(s.Missing, expr.AsSelected(expr.AddNullable(expr.NullableRow(result), expr.ParamOf(expr.NullableRow(result), value.Null[int64]()))))).Where(expr.And(expr.Gt(result, 0), expr.Less(result, expr.ParamOf(result, 100)))), expr.Asc(expr.Negate(f)))
	resultRows := must(rowset.Read(q).All(ctx, db))
	fmt.Fprintln(&g.output, oracleSnapshot([]any{resultRows}))
}
