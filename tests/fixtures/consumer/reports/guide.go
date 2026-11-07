package reports

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin paginate-projection
func SummaryPage(ctx context.Context, db database.Executor, page orm.PageRequest) (orm.PageResult[UserSummary], error) {
	users := models.Users
	query := users.Query().Where(orm.EnumEq(users.Status, models.StatusActive)).
		OrderBy(orm.TextAsc(users.Email), orm.KeyAsc(users.Key()))
	selected := rows.Select(rows.From(query), UserSummaries,
		rows.Map(UserSummaries.ID, expr.Value(orm.KeyExpr(users.Key()))),
		rows.Map(UserSummaries.Email, expr.Value(orm.TextExpr(users.Email))),
		rows.Map(UserSummaries.Nickname, expr.Value(orm.NullableTextExpr(users.Nickname))),
		rows.Map(UserSummaries.Status, expr.Value(orm.EnumExpr(users.Status))),
	)
	return rows.Read(selected).Paginate(ctx, db, page)
} // doc-example:end paginate-projection
