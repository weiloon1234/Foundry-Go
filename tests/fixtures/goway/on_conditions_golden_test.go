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

type onResult struct{ Email value.Nullable[string] }
type onFields struct {
	rowset.Row[onResult]
	Email rowset.Field[onResult, value.Nullable[string]]
}

var onRows = rowset.Define(func(b rowset.Builder[onResult], r *onResult) onFields {
	return onFields{b.Row(), rowset.NullableTextOf(b, &r.Email)}
})

func TestStage6b2OnGolden(t *testing.T) {
	after := &goldenCapture{}
	for _, g := range []*goldenCapture{after} {
		g.scriptedColumns = []string{"email"}
		g.scriptedBatches = [][][]driver.Value{{{nil}, {"right"}}}
	}

	runOnNew(after)
	if after.statements != 1 || !oracleEqual(t, "on_conditions_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func runOnNew(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a, b := rowset.As[joinA](models.Users.Query(), "a"), rowset.As[joinB](models.Users.Query(), "b")
	ai, bi := expr.At(rowset.Scope(a), models.Users.Age), expr.At(rowset.Scope(b), models.Users.Age)
	ae, be := expr.At(rowset.Scope(a), models.Users.Email), expr.At(rowset.Scope(b), models.Users.Email)
	an, bn := expr.At(rowset.Scope(a), models.Users.Nickname), expr.At(rowset.Scope(b), models.Users.Nickname)
	on := rowset.OnAnd(rowset.On(expr.At(rowset.Scope(a), models.Users.IntroducerID), expr.At(rowset.Scope(b), models.Users.ID)), rowset.OnOr(rowset.OnEqual(expr.Add(ai, ai), expr.Add(bi, bi)), rowset.OnNotEqual(ae, be), rowset.OnLess(ai, bi), rowset.OnLessOrEqual(ai, bi), rowset.OnGreater(ai, bi), rowset.OnGreaterOrEqual(ai, bi), rowset.OnLike(ae, be), rowset.OnDistinctFrom(an, bn), rowset.OnNotDistinctFrom(an, bn), rowset.OnLessNullable(expr.NullableRow(ai), expr.NullableRow(bi)), rowset.OnLessOrEqualNullable(expr.NullableRow(ai), expr.NullableRow(bi)), rowset.OnGreaterNullable(expr.NullableRow(ai), expr.NullableRow(bi)), rowset.OnGreaterOrEqualNullable(expr.NullableRow(ai), expr.NullableRow(bi)), rowset.OnLikeNullable(an, bn)).Not()).WhereLeft(expr.OrderRow(ai).Gt(1)).WhereRight(be.Ne("hidden"))
	j := rowset.LeftJoin(a, b, on)
	s := onRows
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Read(rowset.Select(j, s, rowset.Map(s.Email, expr.AtNullable(rowset.NullableRightScope(j, rowset.Scope(b)), models.Users.Email)))).All(ctx, db))}))
}
