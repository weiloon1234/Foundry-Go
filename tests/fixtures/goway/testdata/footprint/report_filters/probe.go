package report_filters

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Compile() (orm.Statement, error) {
	note := expr.TextNullableRow(models.Users.Nickname)
	state := expr.CompareRow(expr.Value(models.Users.Status))
	nullable := expr.CompareNullableRow(expr.Value(models.Users.Nickname))
	p := expr.And(state.In(models.StatusActive), expr.Or(nullable.Ne(""), nullable.IsNull()), expr.Or(expr.Not(note.Contains("Ada")), note.IsNull()), expr.Has(models.UserRelations.Orders, expr.ModelPredicate(models.Orders.TotalCents.Gt(100))))
	return rows.CompileRead(rows.Read(rows.FromModel(models.Users.Query()).Where(p)))
}
