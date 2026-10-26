package result_cursor

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Read(ctx context.Context, db database.Executor) (orm.CursorPage[models.Order], error) {
	c := rows.CursorFor(rows.ModelRecord(models.Orders.Query()))
	return c.UniqueBy(expr.Group(expr.At(c.Scope(), models.Orders.ID))).Paginate(ctx, db, orm.CursorRequest[models.Order]{Size: 10})
}
func Nullable(ctx context.Context, db database.Executor) (orm.CursorPage[value.Nullable[string]], error) {
	c := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Nickname)).Value())))
	return c.UniqueBy(expr.Group(c.Value())).Paginate(ctx, db, orm.CursorRequest[value.Nullable[string]]{Size: 10})
}
