package related_value

import (
	"context"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Run(ctx context.Context, ex database.Executor) ([]rows.Annotated[models.User, int64], error) {
	count := expr.RelatedValue(models.UserRelations.Orders, expr.Count[models.Order]())
	q := expr.OrderBy(expr.Where(models.Users.Query().With(models.UserRelations.Orders), expr.Gte(expr.OrderRow(count), 1)), expr.Desc(count))
	annotated := rows.WithValue(q, count)
	if _, err := rows.CompileRead(annotated); err != nil {
		return nil, err
	}
	return rows.ReadRecord(annotated).All(ctx, ex)
}
