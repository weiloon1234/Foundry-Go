package record_distinct_on

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type buyer struct{}
type purchase struct{}

func Compile() (orm.Statement, error) {
	a, b := rows.As[buyer](models.Users.Query(), "buyer"), rows.As[purchase](models.Orders.Query(), "purchase")
	j := rows.InnerJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(models.Users.ID)), expr.At(rows.Scope(b), orm.ColumnExpr(models.Orders.BuyerID))))
	left, right := rows.LeftScope(j, rows.Scope(a)), rows.RightScope(j, rows.Scope(b))
	id, order := expr.At(left, orm.KeyExpr(models.Users.ID)), expr.At(right, orm.KeyExpr(models.Orders.ID))
	return rows.CompileRead(rows.ReadRecord(rows.DistinctOnRecord(rows.SelectRecord(j, left), expr.Group(id)).OrderBy(id.Asc(), order.Desc())))
}
