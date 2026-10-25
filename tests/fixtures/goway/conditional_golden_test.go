package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"

	"foundry.test/goway/models"
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
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
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, c := range []*goldenCapture{before, after} {
		c.scriptedColumns = []string{"required", "missing"}
		c.scriptedBatches = [][][]driver.Value{{{"", nil}, {"Ada", "Ada"}}}
	}
	runConditionalOld(before)
	runConditionalNew(after)
	if before.statements != 1 || after.statements != 1 || !oracleEqual(t, "conditional_golden/1", before.output.String(), after.output.String()) {
		t.Fatalf("conditional parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
	// Selected CASE also reuses the aggregate compiler, with identical binds.
	before, after = &goldenCapture{}, &goldenCapture{}
	for _, c := range []*goldenCapture{before, after} {
		c.scriptedColumns = []string{"number", "exact", "approx", "missing"}
		c.scriptedBatches = [][][]driver.Value{{{int64(1), "1", float64(1), nil}}}
	}
	runSelectedConditionalOld(before)
	runSelectedConditionalNew(after)
	if before.statements != 1 || after.statements != 1 || !oracleEqual(t, "conditional_golden/2", before.output.String(), after.output.String()) {
		t.Fatalf("selected conditional parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func runConditionalOld(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := query.As[expressionSide](old.QueryUsers(), "u")
	f := old.UserFieldsAt(a.Scope())
	fallback := query.Param(f.Email, "")
	choice := query.When(f.Age.Gt(18), query.Coalesce(f.Nickname, f.Email)).When(f.Age.Gt(10), fallback).Else(f.Email)
	required := query.NewProjectionField[conditionalResult, string]("required")
	missing := query.NewProjectionField[conditionalResult, value.Nullable[string]]("missing")
	schema := query.DefineProjection([]query.ProjectionColumn[conditionalResult]{required.Column(), missing.Column()}, func(row database.Row) (conditionalResult, error) {
		var r conditionalResult
		err := row.Scan(codec.String[string]().Scan(&r.Required), codec.Nullable(codec.String[string]()).Scan(&r.Missing))
		return r, err
	})
	nullable := query.When(f.Age.Gt(0), choice).ElseNull()
	q := query.Project(a, schema, query.Map(required, choice.Value()), query.Map(missing, query.NullIfNullable(query.CoalesceNullable(nullable, f.Nickname), query.NullableRow(fallback)).Value())).Where(query.NullIf(choice, fallback).IsNotNull()).OrderBy(choice.Asc())
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(q.All(ctx, db))}))
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
func runSelectedConditionalOld(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := query.As[expressionSide](old.QueryOrders(), "o")
	count := query.Count[query.Alias[expressionSide, old.Order]]()
	choice := query.WhenValue(count.Gt(0), count.Value()).Else(count.Param(0).Value())
	result := query.CoalesceValue(query.NullIfValue(choice, count.Param(0).Value()), count.Param(0).Value())
	number := query.NewProjectionField[expressionResult, int64]("number")
	exact := query.NewProjectionField[expressionResult, decimal.Decimal]("exact")
	approx := query.NewProjectionField[expressionResult, float64]("approx")
	missing := query.NewProjectionField[expressionResult, value.Nullable[int64]]("missing")
	schema := query.DefineProjection([]query.ProjectionColumn[expressionResult]{number.Column(), exact.Column(), approx.Column(), missing.Column()}, func(row database.Row) (expressionResult, error) {
		var r expressionResult
		err := row.Scan(codec.Signed[int64]().Scan(&r.Number), codec.Decimal().Scan(&r.Exact), codec.Float[float64]().Scan(&r.Approx), codec.Nullable(codec.Signed[int64]()).Scan(&r.Missing))
		return r, err
	})
	q := query.Project(a, schema, query.Map(number, result), query.Map(exact, query.DecimalValue(result)), query.Map(approx, query.FloatValue(result)), query.Map(missing, query.WhenValue(count.Gt(10), count.Value()).ElseNull()))
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
