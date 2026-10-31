package goway_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

type joinA struct{}
type joinB struct{}
type joinC struct{}
type joinResult struct{ LeftEmail, RightEmail, Nickname value.Nullable[string] }
type joinFields struct {
	rowset.Row[joinResult]
	LeftEmail, RightEmail, Nickname rowset.Field[joinResult, value.Nullable[string]]
}

var joinRows = rowset.Define(func(b rowset.Builder[joinResult], r *joinResult) joinFields {
	return joinFields{b.Row(), rowset.NullableTextOf(b, &r.LeftEmail), rowset.NullableTextOf(b, &r.RightEmail), rowset.NullableTextOf(b, &r.Nickname)}
})

func TestStage6b1JoinGolden(t *testing.T) {
	for _, kind := range []string{"Inner", "Left", "Right", "Full", "Cross", "Chain"} {
		t.Run(kind, func(t *testing.T) {
			after := &goldenCapture{}
			for _, g := range []*goldenCapture{after} {
				g.scriptedColumns = []string{"left_email", "right_email", "nickname"}
				g.scriptedBatches = [][][]driver.Value{{{nil, nil, nil}, {"left", "right", "nick"}}}
			}

			runJoinNew(after, kind)
			if after.statements != 1 || !oracleEqual(t, "joins_golden/1", after.output.String()) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}

func runJoinNew(g *goldenCapture, kind string) {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	a := rowset.As[joinA](models.Users.Query().Where(orm.NumberGt(models.Users.Age, 10)).OrderBy(orm.NumberDesc(models.Users.Age)).Limit(5), "a")
	b := rowset.As[joinB](models.Users.Query().Where(orm.TextNe(models.Users.Email, "hidden")), "b")
	on := rowset.On(expr.At(rowset.Scope(a), orm.NullableColumnExpr(models.Users.IntroducerID)), expr.At(rowset.Scope(b), orm.KeyExpr(models.Users.ID)))
	schema := joinRows
	switch kind {
	case "Inner":
		j := rowset.InnerJoin(a, b, on)
		ls := rowset.LeftScope(j, rowset.Scope(a))
		rs := rowset.RightScope(j, rowset.Scope(b))
		lf := expr.At(ls, orm.TextExpr(models.Users.Email))
		rf := expr.At(rs, orm.TextExpr(models.Users.Email))
		nick := expr.At(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.NullableRow(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.NullableRow(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	case "Left":
		j := rowset.LeftJoin(a, b, on)
		ls := rowset.LeftScope(j, rowset.Scope(a))
		rs := rowset.NullableRightScope(j, rowset.Scope(b))
		lf := expr.At(ls, orm.TextExpr(models.Users.Email))
		rf := expr.AtNullable(rs, orm.TextExpr(models.Users.Email))
		nick := expr.AtNullableValue(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.NullableRow(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.Row(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	case "Right":
		j := rowset.RightJoin(a, b, on)
		ls := rowset.NullableLeftScope(j, rowset.Scope(a))
		rs := rowset.RightScope(j, rowset.Scope(b))
		lf := expr.AtNullable(ls, orm.TextExpr(models.Users.Email))
		rf := expr.At(rs, orm.TextExpr(models.Users.Email))
		nick := expr.At(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.Row(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.NullableRow(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	case "Full":
		j := rowset.FullJoin(a, b, on)
		ls := rowset.NullableLeftScope(j, rowset.Scope(a))
		rs := rowset.NullableRightScope(j, rowset.Scope(b))
		lf := expr.AtNullable(ls, orm.TextExpr(models.Users.Email))
		rf := expr.AtNullable(rs, orm.TextExpr(models.Users.Email))
		nick := expr.AtNullableValue(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.Row(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.Row(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	case "Cross":
		j := rowset.CrossJoin(a, b)
		ls := rowset.LeftScope(j, rowset.Scope(a))
		rs := rowset.RightScope(j, rowset.Scope(b))
		lf := expr.At(ls, orm.TextExpr(models.Users.Email))
		rf := expr.At(rs, orm.TextExpr(models.Users.Email))
		nick := expr.At(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.NullableRow(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.NullableRow(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	case "Chain":
		base := rowset.LeftJoin(a, b, on)
		c := rowset.As[joinC](models.Users.Query(), "c")
		j := rowset.CrossJoin(base, c)
		ls := rowset.LeftScope(j, rowset.LeftScope(base, rowset.Scope(a)))
		rs := rowset.LeftNullableScope(j, rowset.NullableRightScope(base, rowset.Scope(b)))
		lf := expr.At(ls, orm.TextExpr(models.Users.Email))
		rf := expr.AtNullable(rs, orm.TextExpr(models.Users.Email))
		nick := expr.AtNullableValue(rs, orm.NullableTextExpr(models.Users.Nickname))
		got := must(rowset.Read(rowset.OrderBy(rowset.Select(j, schema, rowset.MapSelected(schema.LeftEmail, expr.AsSelected(expr.NullableRow(lf))), rowset.MapSelected(schema.RightEmail, expr.AsSelected(expr.Row(rf))), rowset.Map(schema.Nickname, nick)), expr.Asc(lf))).All(ctx, db))
		fmt.Fprintln(&g.output, oracleSnapshot([]any{got}))
	}
}
