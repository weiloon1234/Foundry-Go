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
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, g := range []*goldenCapture{before, after} {
		g.scriptedColumns = []string{"email"}
		g.scriptedBatches = [][][]driver.Value{{{nil}, {"right"}}}
	}
	runOnOld(before)
	runOnNew(after)
	if before.statements != 1 || after.statements != 1 || !oracleEqual(t, "on_conditions_golden/1", before.output.String(), after.output.String()) {
		t.Fatalf("ON parity:\n%s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func runOnOld(g *goldenCapture) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a, b := query.As[joinA](old.QueryUsers(), "a"), query.As[joinB](old.QueryUsers(), "b")
	af, bf := old.UserFieldsAt(a.Scope()), old.UserFieldsAt(b.Scope())
	on := query.OnAnd(query.On(af.IntroducerID, bf.ID), query.OnOr(query.OnEqual(query.Add(af.Age, af.Age), query.Add(bf.Age, bf.Age)), query.OnNotEqual(af.Email, bf.Email), query.OnLess(af.Age, bf.Age), query.OnLessOrEqual(af.Age, bf.Age), query.OnGreater(af.Age, bf.Age), query.OnGreaterOrEqual(af.Age, bf.Age), query.OnLike(af.Email, bf.Email), query.OnDistinctFrom(af.Nickname, bf.Nickname), query.OnNotDistinctFrom(af.Nickname, bf.Nickname), query.OnLessNullable(query.NullableRow(af.Age), query.NullableRow(bf.Age)), query.OnLessOrEqualNullable(query.NullableRow(af.Age), query.NullableRow(bf.Age)), query.OnGreaterNullable(query.NullableRow(af.Age), query.NullableRow(bf.Age)), query.OnGreaterOrEqualNullable(query.NullableRow(af.Age), query.NullableRow(bf.Age)), query.OnLikeNullable(af.Nickname, bf.Nickname)).Not()).WhereLeft(af.Age.Gt(1)).WhereRight(bf.Email.Ne("hidden"))
	j := query.LeftJoin(a, b, on)
	f := old.UserNullableFieldsAt(query.NullableRightScope(j, b.Scope()))
	field := query.NewProjectionField[onResult, value.Nullable[string]]("email")
	schema := query.DefineProjection([]query.ProjectionColumn[onResult]{field.Column()}, func(row database.Row) (onResult, error) {
		var r onResult
		err := row.Scan(codec.Nullable(codec.String[string]()).Scan(&r.Email))
		return r, err
	})
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(query.Project(j, schema, query.Map(field, query.Value(f.Email))).All(ctx, db))}))
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
	fmt.Fprintln(&g.output, oracleSnapshot([]any{must(rowset.Select(j, s, rowset.Map(s.Email, expr.AtNullable(rowset.NullableRightScope(j, rowset.Scope(b)), models.Users.Email))).All(ctx, db))}))
}
