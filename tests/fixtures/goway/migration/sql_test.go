package migration_test

import (
	"context"
	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	"foundry.test/goway/models/jsonpaths"

	reports "foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/value"
)

// migration:conflict-after
func upsert(ctx context.Context, tx *database.Tx) error {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Age))
	_, err := orm.Upsert(ctx, tx, f.Query(), policy, f.Email.Set("ada@example.test"), f.Age.Set(30))
	return err
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

// migration:source-update-after
func updateFrom(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	_, err := rows.UpdateFrom(f.Query(), rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).Values(f.Name.Set("updated")).Exec(ctx, tx)
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

// migration:cte-after
func cte(ctx context.Context, db database.Executor) error {
	cte := rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(models.Orders.TotalCents.Gt(100))))
	a := rows.AsRecord[orderSide](cte, "o")
	_, err := rows.SelectRecord(a, rows.Scope(a)).All(ctx, db)
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

// migration:json-after
var Theme = expr.JSONFieldOf(func(p *jsonpaths.Preferences) *value.Optional[string] { return &p.Theme })

func jsonTheme() expr.NullableTextRowExpression[jsonpaths.Document, string] {
	root := expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
	theme := expr.JSONOptional(expr.JSONProperty(root, Theme))
	return expr.JSONTextScalar(theme, codec.String[string]())
}

// migration:end

// migration:projection-after
func report() rows.Query[reports.Member, reports.MemberRow] {
	f, r := reports.Members, reports.MemberRows
	return rows.Select(rows.From(f.Query()), r,
		rows.Map(r.ID, expr.Value(f.ID)), rows.Map(r.Name, expr.Value(f.Name)),
		rows.Map(r.Nickname, expr.Value(f.Nickname)), rows.Map(r.State, expr.Value(f.State)),
		rows.Map(r.Balance, expr.Value(f.Balance)))
}

// migration:end

// migration:datatable-after
func tableSource[A any](spec datatable.Spec[reports.Member, reports.MemberRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[reports.Member, reports.MemberRow, A] {
	// Keep the existing Spec authorization; tenant resolves the trusted actor scope.
	spec.Source = func(ctx context.Context, actor A) (rows.Query[reports.Member, reports.MemberRow], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return rows.Query[reports.Member, reports.MemberRow]{}, err
		}
		return report().Where(expr.Value(reports.Members.TenantID).Eq(id)), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Value(reports.Members.ID).Asc()}
	return spec
}

// migration:end
