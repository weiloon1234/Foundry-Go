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

// doc-example:begin conflict-after
func upsert(ctx context.Context, tx *database.Tx) error {
	f := models.Users
	policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Age))
	_, err := orm.Upsert(ctx, tx, f.Query(), policy, orm.Set(f.Email, "ada@example.test"), orm.Set(f.Age, 30))
	return err
}

// doc-example:end conflict-after

// doc-example:begin conflict-stored-plus-proposed
func excluded() orm.Conflict[models.User] {
	r := rows.ConflictRowsOf(models.Users.Query())
	stored := expr.At(r.Stored(), models.Users.Age)
	proposed := expr.At(r.Proposed(), models.Users.Age)
	return orm.OnConflict(orm.ConflictKey(models.Users.Email)).DoUpdate(
		rows.SetConflictValue(models.Users.Age, expr.Add(stored, proposed)))
}

// doc-example:end conflict-stored-plus-proposed

// doc-example:begin source-insert-after
func insertSelect(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	plan := rows.InsertFrom(rows.From(f.Query()), f.Query(),
		rows.MapInsert(f.ID, expr.Value(f.Key())), rows.MapInsert(f.Body, expr.Value(f.Body)),
		rows.MapInsert(f.CreatedAt, expr.Value(f.CreatedAt))).Values(orm.Set(f.Name, "copy"))
	_, err := plan.Exec(ctx, tx)
	return err
}

// doc-example:end source-insert-after

// doc-example:begin source-update-after
func updateFrom(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	_, err := rows.UpdateFrom(f.Query(), rows.From(f.Query())).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).
		Select(rows.MapUpdate(f.Body, expr.Value(f.Body))).Values(orm.Set(f.Name, "updated")).Exec(ctx, tx)
	return err
}

// doc-example:end source-update-after

// doc-example:begin source-delete-after
func deleteUsing(ctx context.Context, tx *database.Tx) error {
	f := hooks.Members
	q := f.Query().WithTrashed()
	_, err := rows.ForceDeleteUsing(q, f.Soft, rows.From(q)).
		Match(rows.MatchSource(f.Key(), expr.Value(f.Key()))).Exec(ctx, tx)
	return err
}

// doc-example:end source-delete-after

// doc-example:begin aggregate-after
func aggregate(ctx context.Context, db database.Executor) error {
	count := expr.Count[models.Order]()
	q := rows.GroupByScalar(rows.SelectScalar(rows.From(models.Orders.Query()), expr.AsSelected(count)),
		expr.Group(expr.Value(models.Orders.BuyerID)))
	_, err := rows.ReadScalar(rows.HavingScalar(q, expr.GtValue(count, 1))).All(ctx, db)
	return err
}

// doc-example:end aggregate-after

type orderSide struct{}

// doc-example:begin window-after
func window(ctx context.Context, db database.Executor) error {
	a := rows.As[orderSide](models.Orders.Query(), "o")
	s := rows.Scope(a)
	window := expr.WindowFor(s).PartitionBy(expr.Group(expr.At(s, models.Orders.BuyerID))).
		OrderBy(expr.Desc(expr.At(s, models.Orders.TotalCents)))
	_, err := rows.ReadScalar(rows.SelectScalar(a, expr.RowNumber(window))).All(ctx, db)
	return err
}

// doc-example:end window-after

// doc-example:begin cte-after
func cte(ctx context.Context, db database.Executor) error {
	cte := rows.CTE("eligible", rows.ModelRecord(models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100))))
	a := rows.AsRecord[orderSide](cte, "o")
	_, err := rows.ReadRecord(rows.SelectRecord(a, rows.Scope(a))).All(ctx, db)
	return err
}

// doc-example:end cte-after

// doc-example:begin sets-after
func sets(ctx context.Context, db database.Executor) error {
	a := models.Orders.Query().Where(orm.Gt(models.Orders.TotalCents, 100))
	_, err := rows.ReadSet(rows.Union(rows.ModelRecord(a), rows.ModelRecord(models.Orders.Query()))).All(ctx, db)
	if err != nil {
		return err
	}
	_, err = rows.Read(rows.Distinct(rows.FromModel(models.Orders.Query()))).All(ctx, db)
	return err
}

// doc-example:end sets-after

// doc-example:begin json-after
var Theme = expr.JSONFieldOf(func(p *jsonpaths.Preferences) *value.Optional[string] { return &p.Theme })

func jsonTheme() expr.NullableTextRowExpression[jsonpaths.Document, string] {
	root := expr.JSONRoot(expr.Value(jsonpaths.Documents.Settings))
	theme := expr.JSONOptional(expr.JSONProperty(root, Theme))
	return expr.JSONTextScalar(theme, codec.String[string]())
}

// doc-example:end json-after

// doc-example:begin projection-after
func report() rows.Query[reports.Member, reports.MemberRow] {
	f, r := reports.Members, reports.MemberRows
	return rows.Select(rows.From(f.Query()), r,
		rows.Map(r.ID, expr.Value(f.ID)), rows.Map(r.Name, expr.Value(f.Name)),
		rows.Map(r.Nickname, expr.Value(f.Nickname)), rows.Map(r.State, expr.Value(f.State)),
		rows.Map(r.Balance, expr.Value(f.Balance)))
}

// doc-example:end projection-after

// doc-example:begin datatable-after
func tableSource[A any](spec datatable.Spec[reports.Member, reports.MemberRow, A], tenant func(context.Context, A) (int64, error)) datatable.Spec[reports.Member, reports.MemberRow, A] {
	// Keep the existing Spec authorization; tenant resolves the trusted actor scope.
	spec.Source = func(ctx context.Context, actor A) (rows.SelectionSource[reports.Member], error) {
		id, err := tenant(ctx, actor)
		if err != nil {
			return nil, err
		}
		return rows.From(reports.Members.Query().Where(orm.Eq(reports.Members.TenantID, id))), nil
	}
	spec.Stable = []expr.ProjectionOrder[reports.Member]{expr.Asc(expr.Value(reports.Members.ID))}
	return spec
}

// doc-example:end datatable-after
