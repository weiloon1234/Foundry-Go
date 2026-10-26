package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
)

type conditionalResult struct {
	Required string
	Missing  value.Nullable[string]
}
type conditionalFields struct {
	rowset.Row[conditionalResult]
	Required rowset.Field[conditionalResult, string]
	Missing  rowset.Field[conditionalResult, value.Nullable[string]]
}

var conditionalRows = rowset.Define(func(b rowset.Builder[conditionalResult], r *conditionalResult) conditionalFields {
	return conditionalFields{b.Row(), rowset.TextOf(b, &r.Required), rowset.NullableTextOf(b, &r.Missing)}
})

func TestStage6a2ConditionalGolden(t *testing.T) {
	after := &goldenCapture{}
	for _, c := range []*goldenCapture{after} {
		c.scriptedColumns = []string{"required", "missing"}
		c.scriptedBatches = [][][]driver.Value{{{"", nil}, {"Ada", "Ada"}}}
	}

	runConditionalNew(after)
	if after.statements != 1 || !oracleEqual(t, "conditional_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}

	// Selected CASE also reuses the aggregate compiler, with identical binds.
	after = &goldenCapture{}
	for _, c := range []*goldenCapture{after} {
		c.scriptedColumns = []string{"number", "exact", "approx", "missing"}
		c.scriptedBatches = [][][]driver.Value{{{int64(1), "1", float64(1), nil}}}
	}

	runSelectedConditionalNew(after)
	if after.statements != 1 || !oracleEqual(t, "conditional_golden/2", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func runConditionalNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[expressionSide](models.Users.Query(), "u")
	age := expr.OrderRow(expr.At(rowset.Scope(a), models.Users.Age))
	email := expr.At(rowset.Scope(a), models.Users.Email)
	nickname := expr.At(rowset.Scope(a), models.Users.Nickname)
	fallback := expr.Select(email).Param("")
	choice := expr.When(age.Gt(18), expr.Coalesce(nickname, email)).When(age.Gt(10), fallback).Else(email)
	nullable := expr.When(age.Gt(0), choice).ElseNull()
	s := conditionalRows
	q := rowset.Select(a, s, rowset.MapSelected(s.Required, choice.Value()), rowset.MapSelected(s.Missing, expr.NullIfNullable(expr.CoalesceNullable(nullable, nickname), expr.NullableRow(fallback)).Value())).Where(expr.NullIf(choice, fallback).IsNotNull()).OrderBy(choice.Asc())
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.All(ctx, db))}))
}

func runSelectedConditionalNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[expressionSide](models.Orders.Query(), "o")
	count := expr.Count[rowset.Alias[expressionSide, models.Order]]()
	choice := expr.WhenValue(count.Gt(0), count.Value()).Else(count.Param(0).Value())
	result := expr.CoalesceValue(expr.NullIfValue(choice, count.Param(0).Value()), count.Param(0).Value())
	s := expressionRows
	q := rowset.Select(a, s, rowset.MapSelected(s.Number, result), rowset.MapSelected(s.Exact, expr.DecimalValue(result)), rowset.MapSelected(s.Approx, expr.FloatValue(result)), rowset.MapSelected(s.Missing, expr.WhenValue(count.Gt(10), count.Value()).ElseNull()))
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.All(ctx, db))}))
}
