package migration_test

import (
	"context"
	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	"foundry.test/goway/models/jsonpaths"
	old "foundry.test/goway/oldmodels"
	oldhooks "foundry.test/goway/oldmodels/hooks"
	reports "foundry.test/goway/oldreporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/value"
)

// migration:conflict-before
func legacyUpsert(ctx context.Context, tx *database.Tx) error {
	f := old.UserFields()
	policy := query.OnConflict(f.Email).DoUpdate(f.Age.Incoming())
	_, err := old.QueryUsers().Upsert(ctx, tx, old.UserDraft{}.SetEmail("ada@example.test").SetAge(30), policy)
	return err
}

// migration:end
// migration:conflict-after
func upsert(ctx context.Context, tx *database.Tx) error {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Age))
	_, err := orm.Upsert(ctx, tx, f.Query(), policy, f.Email.Set("ada@example.test"), f.Age.Set(30))
	return err
}

// migration:end

// migration:excluded-before
func legacyExcluded() query.Conflict[old.User] {
	q := old.QueryUsers()
	r := q.ConflictRows()
	stored, proposed := old.UserFieldsAt(r.Stored()), old.UserFieldsAt(r.Proposed())
	return query.OnConflict(old.UserFields().Email).DoUpdate(
		query.SetConflictValue(old.UserFields().Age, query.Add(stored.Age, proposed.Age)))
}

// migration:end
// migration:excluded-after
func excluded() orm.Conflict[models.User] {
	r := rows.ConflictRowsOf(models.Users.Query())
	stored := expr.At(r.Stored(), models.Users.Age)
	proposed := expr.At(r.Proposed(), models.Users.Age)
	return orm.OnConflict(orm.ConflictKey(models.Users.Email)).DoUpdate(
		rows.SetConflictValue(models.Users.Age, expr.Add(stored, proposed)))
}

// migration:end

// migration:source-insert-before
func legacyInsertSelect(ctx context.Context, tx *database.Tx) error {
	f := oldhooks.MemberFields()
	plan := oldhooks.InsertMemberFrom(oldhooks.QueryHookMembers()).
		SelectID(query.Value(f.ID)).SelectBody(query.Value(f.Body)).SelectCreatedAt(query.Value(f.CreatedAt)).
		Values(oldhooks.MemberDraft{}.SetName("copy"))
	_, err := plan.Exec(ctx, tx)
	return err
}

// migration:end
// migration:source-insert-after
func insertSelect(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	plan := rows.InsertFrom(rows.From(f.Query()), f.Query(),
		rows.MapInsert(f.ID, expr.Value(f.Key())), rows.MapInsert(f.Body, expr.Value(f.Body)),
		rows.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt))).Values(f.Name.Set("copy"))
	_, err := plan.Exec(ctx, tx)
	return err
}

// migration:end

// migration:source-update-before
func legacyUpdateFrom(ctx context.Context, tx *database.Tx) error {
	f, q := oldhooks.MemberFields(), oldhooks.QueryHookMembers()
	_, err := oldhooks.UpdateMemberFrom(q, q).MatchID(query.Value(f.ID)).
		SelectBody(query.Value(f.Body)).Values(oldhooks.MemberDraft{}.SetName("updated")).Exec(ctx, tx)
	return err
}

// migration:end
// migration:source-update-after
func updateFrom(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	_, err := rows.UpdateFrom(f.Query(), rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).Values(f.Name.Set("updated")).Exec(ctx, tx)
	return err
}

// migration:end

// migration:source-delete-before
func legacyDeleteUsing(ctx context.Context, tx *database.Tx) error {
	q := oldhooks.QueryHookMembers().WithTrashed()
	_, err := oldhooks.ForceDeleteMemberUsing(q, q).MatchID(query.Value(oldhooks.MemberFields().ID)).Exec(ctx, tx)
	return err
}

// migration:end
// migration:source-delete-after
func deleteUsing(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	q := f.Query().WithTrashed()
	_, err := rows.ForceDeleteUsing(q, f.Soft, rows.From(q)).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).Exec(ctx, tx)
	return err
}

// migration:end

// migration:aggregate-before
func legacyAggregate(ctx context.Context, db database.Executor) error {
	count := query.Count[old.Order]()
	q := query.SelectValue(old.QueryOrders(), count.Value()).
		GroupBy(old.OrderFields().BuyerID.Group()).Having(count.Gt(1))
	_, err := q.All(ctx, db)
	return err
}

// migration:end
// migration:aggregate-after
func aggregate(ctx context.Context, db database.Executor) error {
	count := expr.Count[models.Order]()
	q := rows.GroupByScalar(rows.SelectScalar(rows.From(models.Orders.Query()), count.Value()),
		expr.Group(expr.Value(models.Orders.BuyerID)))
	_, err := rows.ReadScalar(rows.HavingScalar(q, count.Gt(1))).All(ctx, db)
	return err
}

// migration:end

type orderSide struct{}

// migration:window-before
func legacyWindow(ctx context.Context, db database.Executor) error {
	a := query.As[orderSide](old.QueryOrders(), "o")
	f := old.OrderFieldsAt(a.Scope())
	window := query.WindowFor(a).PartitionBy(f.BuyerID.Group()).OrderBy(f.TotalCents.Desc())
	_, err := query.SelectValue(a, query.RowNumber(window)).All(ctx, db)
	return err
}

// migration:end
// migration:window-after
func window(ctx context.Context, db database.Executor) error {
	a := rows.As[orderSide](models.Orders.Query(), "o")
	s := rows.Scope(a)
	window := expr.WindowFor(s).PartitionBy(expr.Group(expr.At(s, models.Orders.BuyerID))).
		OrderBy(expr.At(s, models.Orders.TotalCents).Desc())
	_, err := rows.ReadScalar(rows.SelectScalar(a, expr.RowNumber(window))).All(ctx, db)
	return err
}

// migration:end

// migration:cte-before
func legacyCTE(ctx context.Context, db database.Executor) error {
	cte := query.CTE("eligible", old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100)))
	a := query.As[orderSide](cte, "o")
	_, err := query.SelectRecord(a, a.Scope()).All(ctx, db)
	return err
}

// migration:end
// migration:cte-after
func cte(ctx context.Context, db database.Executor) error {
	cte := rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(100))))
	a := rows.AsRecord[orderSide](cte, "o")
	_, err := rows.SelectRecord(a, rows.Scope(a)).All(ctx, db)
	return err
}

// migration:end

// migration:sets-before
func legacySets(ctx context.Context, db database.Executor) error {
	a := old.QueryOrders().Where(old.OrderFields().TotalCents.Gt(100))
	_, err := query.Union(a, old.QueryOrders()).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = old.QueryOrders().Distinct().All(ctx, db)
	return err
}

// migration:end
// migration:sets-after
func sets(ctx context.Context, db database.Executor) error {
	a := models.Orders.Query().Where(models.Orders.TotalCents.Gt(100))
	_, err := rows.Union(rows.ModelRecord(a), rows.ModelRecord(models.Orders.Query())).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = rows.Distinct(rows.FromModel(models.Orders.Query())).All(ctx, db)
	return err
}

// migration:end

// migration:json-before
func legacyJSON() query.NullableTextRowExpression[jsonpaths.Document, string] {
	field := query.NewJSONField[jsonpaths.Document, value.JSON[jsonpaths.Preferences]]("json_documents", "settings", codec.JSON[jsonpaths.Preferences]())
	theme := query.NewJSONProperty[jsonpaths.Document, jsonpaths.Preferences, string](query.JSONRoot(field), "theme", false)
	return query.JSONTextScalar(theme, codec.String[string]())
}

// migration:end
// migration:json-after
var Theme = expr.JSONFieldOf(func(p *jsonpaths.Preferences) *value.Optional[string] { return &p.Theme })

func jsonTheme() expr.NullableTextRowExpression[jsonpaths.Document, string] {
	root := expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
	theme := expr.JSONOptional(expr.JSONProperty(root, Theme))
	return expr.JSONTextScalar(theme, codec.String[string]())
}

// migration:end

// migration:projection-before
func legacyReport() query.ProjectionQuery[reports.Member, reports.MemberRow] {
	f := reports.MemberFields()
	return reports.ProjectMemberRow(reports.QueryReportMembers()).
		SelectID(query.Value(f.ID)).SelectName(query.Value(f.Name)).SelectNickname(query.Value(f.Nickname)).
		SelectState(query.Value(f.State)).SelectBalance(query.Value(f.Balance)).Query()
}

// migration:end
// migration:projection-after
func report() rows.Query[reports.Member, reports.MemberRow] {
	f, r := reports.MemberModels, reports.MemberRows
	return rows.Select(rows.From(f.Query()), r,
		rows.Map(r.ID, expr.Value(f.ID)), rows.Map(r.Name, expr.Value(f.Name)),
		rows.Map(r.Nickname, expr.Value(f.Nickname)), rows.Map(r.State, expr.Value(f.State)),
		rows.Map(r.Balance, expr.Value(f.Balance)))
}

// migration:end

// migration:datatable-before
func legacyTableSource(ctx context.Context, actor reports.Authority, tenant func(context.Context, reports.Authority) (int64, error)) (query.ProjectionQuery[reports.Member, reports.MemberRow], error) {
	id, err := tenant(ctx, actor)
	if err != nil {
		return query.ProjectionQuery[reports.Member, reports.MemberRow]{}, err
	}
	return legacyReport().Where(reports.MemberFields().TenantID.Eq(id)), nil
}

// migration:end
// migration:datatable-after
func tableSource(spec datatable.Spec[reports.Member, reports.MemberRow, reports.Authority], tenant func(context.Context, reports.Authority) (int64, error)) datatable.Spec[reports.Member, reports.MemberRow, reports.Authority] {
	// Keep the existing Spec authorization; tenant resolves the trusted actor scope.
	spec.Source = func(ctx context.Context, actor reports.Authority) (rows.Query[reports.Member, reports.MemberRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[reports.Member, reports.MemberRow]{}, err
		}
		return report().Where(expr.Value(reports.MemberModels.TenantID).Eq(id)), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Value(reports.MemberModels.ID).Asc()}
	return spec
}

// migration:end
