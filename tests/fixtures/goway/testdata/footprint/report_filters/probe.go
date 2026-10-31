package report_filters

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Compile() (orm.Statement, error) {
	note := expr.TextNullableRow(orm.NullableTextExpr(models.Users.Nickname))
	state := expr.CompareRow(expr.Value(orm.EnumExpr(models.Users.Status)))
	nullable := expr.CompareNullableRow(expr.Value(orm.NullableTextExpr(models.Users.Nickname)))
	p := expr.And(expr.In(state, models.StatusActive), expr.Or(expr.NullableNe(nullable, ""), expr.IsNull(nullable)), expr.Or(expr.Not(expr.NullableTextContains(note, "Ada")), expr.IsNull(note)), expr.Has(models.UserRelations.Orders, expr.ModelPredicate(orm.NumberGt(models.Orders.TotalCents, 100))))
	return rows.CompileRead(rows.Read(rows.FromModel(models.Users.Query()).Where(p)))
}
