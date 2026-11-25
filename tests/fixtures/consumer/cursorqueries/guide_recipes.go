package cursorqueries

import (
	"cmp"
	"context"
	"slices"

	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin result-cursor-pagination-1
func paginateSummaries(ctx context.Context, db database.Executor, users models.UserTable, summaries reports.UserSummaryColumns) (orm.CursorPage[reports.UserSummary], error) {
	u, r := users, summaries
	report := rows.Select(rows.From(u.Query()), r.Row,
		rows.Map(r.ID, expr.Value(u.Key())), rows.Map(r.Email, expr.Value(u.Email)),
		rows.Map(r.Nickname, expr.Value(u.Nickname)), rows.Map(r.Status, expr.Value(u.Status)))
	cursor := rows.CursorFor(rows.RecordOf(report))
	ordered := cursor.OrderBy(expr.Asc(rows.At(cursor.Scope(), r.Nickname))).
		UniqueBy(expr.Group(rows.At(cursor.Scope(), r.ID)))
	page, err := ordered.Paginate(ctx, db, orm.CursorRequest[reports.UserSummary]{Size: 20})
	return page, err
}

// doc-example:end result-cursor-pagination-1
// doc-example:begin result-cursor-pagination-2
func paginateNicknames(ctx context.Context, db database.Executor, users models.UserTable) (orm.CursorPage[value.Nullable[string]], error) {
	values := rows.DistinctScalar(rows.SelectScalar(rows.From(users.Query()),
		expr.AsSelected(expr.Row(expr.Value(users.Nickname)))))
	cursor := rows.ValueCursorFor(rows.Values(values))
	ordered := cursor.OrderBy(expr.Asc(cursor.Value())).UniqueBy(expr.Group(cursor.Value()))
	page, err := ordered.Paginate(ctx, db, orm.CursorRequest[value.Nullable[string]]{Size: 20})
	return page, err
}

// doc-example:end result-cursor-pagination-2
// doc-example:begin result-cursor-pagination-3
func sortPageByEmail(page orm.CursorPage[reports.UserSummary]) []reports.UserSummary {
	items := slices.Clone(page.Items)
	slices.SortFunc(items, func(a, b reports.UserSummary) int {
		return cmp.Compare(a.Email, b.Email)
	})
	return items
}

// doc-example:end result-cursor-pagination-3
