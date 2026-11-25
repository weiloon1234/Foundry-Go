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
	query := users.Query().Where(orm.Eq(users.Status, models.StatusActive)).
		OrderBy(orm.Asc(users.Email), orm.Asc(users.Key()))
	selected := rows.Select(rows.From(query), UserSummaries,
		rows.Map(UserSummaries.ID, expr.Value(users.Key())),
		rows.Map(UserSummaries.Email, expr.Value(users.Email)),
		rows.Map(UserSummaries.Nickname, expr.Value(users.Nickname)),
		rows.Map(UserSummaries.Status, expr.Value(users.Status)),
	)
	return rows.Read(selected).Paginate(ctx, db, page)
}

// doc-example:end paginate-projection
