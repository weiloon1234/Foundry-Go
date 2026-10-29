package filterqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresFilteredGroupsAndWindows(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.Orders
		base := models.Orders.Query()
		filtered := expr.Sum(orm.NumberExpr(o.TotalCents)).Filter(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 1)))
		q := rowset.Having(rowset.GroupBy(rowset.Select(rowset.From(base), reports.BuyerTotalRows.Row,
			rowset.Map(reports.BuyerTotalRows.BuyerID, expr.Value(orm.ColumnExpr(o.BuyerID))),
			rowset.MapSelected(reports.BuyerTotalRows.Total, filtered.Value()),
			rowset.MapSelected(reports.BuyerTotalRows.Orders, expr.Count[models.Order]().Value()),
			rowset.MapSelected(reports.BuyerTotalRows.Average, expr.Avg(orm.NumberExpr(o.TotalCents)).Filter(expr.ModelPredicate(orm.NumberLt(o.TotalCents, 3))).Value()),
		), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID)))), filtered.Gt(decimal.FromInt64(1)))
		rows, err := rowset.Read(q).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			t.Fatal("filtered measures removed groups")
		}
		for _, row := range rows {
			if row.BuyerID == users[0].ID {
				if row.Orders != 2 || row.Total != value.Of(decimal.FromInt64(2)) {
					t.Fatal("filter affected unfiltered count", row)
				}
				average, ok := row.Average.Get()
				if !ok || average.String() != "1.5" {
					t.Fatal("average lost exact codec", row)
				}
			} else if row.Total != value.Of(decimal.FromInt64(3)) || !row.Average.IsNull() || row.Orders != 1 {
				t.Fatal("empty filtered average did not remain NULL", row)
			}
		}
		w := expr.WindowFor(rowset.ModelScope(base)).OrderBy(expr.Value(orm.NumberExpr(o.TotalCents)).Asc()).RowsBetween(expr.UnboundedPreceding(), expr.CurrentRow())
		window := rowset.OrderBy(rowset.Select(rowset.From(base), windowqueries.WindowTotals.Row,
			rowset.Map(windowqueries.WindowTotals.Amount, expr.Value(orm.NumberExpr(o.TotalCents))),
			rowset.MapSelected(windowqueries.WindowTotals.Running, expr.Over(filtered, w)),
			rowset.MapSelected(windowqueries.WindowTotals.Count, expr.Over(expr.Count[models.Order]().Filter(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 1))), w)),
			rowset.MapSelected(windowqueries.WindowTotals.Any, expr.Over(expr.Exists[models.Order]().Filter(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 2))), w)),
		), expr.Value(orm.NumberExpr(o.TotalCents)).Asc())
		values, err := rowset.Read(window).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(values) != 3 {
			t.Fatal("window filter removed result rows")
		}
		for i, row := range values {
			want := []value.Nullable[decimal.Decimal]{value.Null[decimal.Decimal](), value.Of(decimal.FromInt64(2)), value.Of(decimal.FromInt64(5))}[i]
			if row.Running != want || row.Count != int64(i) || row.Any != (i == 2) {
				t.Fatal("filtered window changed frame/result semantics", row)
			}
		}
		// A FILTER over grouped window input may inspect only grouped columns.
		grouped := expr.Over(expr.Count[models.Order]().Filter(expr.ModelPredicate(orm.ColumnEq(o.BuyerID, users[0].ID))), expr.WindowFor(rowset.ModelScope(base)))
		if got, err := rowset.ReadScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), grouped), expr.Group(expr.Value(orm.ColumnExpr(o.BuyerID))))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("grouped filtered window", got, err)
		}
		return nil
	})
}

func TestPostgresFilteredNullsAndRelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.Users, models.Orders
		people := models.Users.Query()
		for name, computation := range map[string]expr.OrderedAggregate[models.User, int64]{
			"non-null": expr.CountField(orm.NullableTextExpr(u.Nickname)).Filter(expr.ModelPredicate(orm.NullableTextLike(u.Nickname, "B%"))),
			"distinct": expr.CountDistinctField(orm.NullableTextExpr(u.Nickname)).Filter(expr.ModelPredicate(orm.NumberGt(u.Age, 0))),
			"unknown":  expr.Count[models.User]().Filter(expr.ModelPredicate(orm.NullableTextLike(u.Nickname, "Z%"))),
		} {
			got, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(people), computation.Value())).RequireFirst(t.Context(), tx)
			want := int64(1)
			if name == "unknown" {
				want = 0
			}
			if err != nil || got != want {
				t.Fatal(name, got, err)
			}
		}
		if got, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(people), expr.Min(orm.NumberExpr(u.Age)).Filter(expr.ModelPredicate(orm.NumberLt(u.Age, 0))).Value())).RequireFirst(t.Context(), tx); err != nil || !got.IsNull() {
			t.Fatal("filtered empty extrema", got, err)
		}
		if got, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(people), expr.Exists[models.User]().Filter(expr.ModelPredicate(orm.NumberLt(u.Age, 0))).Value())).RequireFirst(t.Context(), tx); err != nil || got {
			t.Fatal("filtered empty existence", err)
		}
		aggregates, relations := models.Users, models.UserLinks
		count := aggregates.OrderCount.Using(expr.Related(relations.Orders, expr.Count[models.Order]().Filter(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 2)))))
		total := aggregates.OrderTotal.Using(expr.Related(relations.Orders, expr.Sum(orm.NumberExpr(o.TotalCents)).Filter(expr.ModelPredicate(orm.NumberGt(o.TotalCents, 2)))))
		rows, err := orm.All(t.Context(), tx, people.With(count, total).OrderBy(orm.TextAsc(u.Email)))
		if err != nil {
			return err
		}
		if len(rows) != 3 {
			t.Fatal("relation filters removed parents")
		}
		for i, row := range rows {
			count, loaded := row.OrderCount.Get()
			total, totalLoaded := row.OrderTotal.Get()
			if !loaded || !totalLoaded {
				t.Fatal("filtered relation omitted loaded state")
			}
			if i == 1 {
				if count != 1 || total != value.Of(decimal.FromInt64(3)) {
					t.Fatal("filtered relation measures", row)
				}
			} else if count != 0 || !total.IsNull() {
				t.Fatal("empty related filter lost defaults", row)
			}
		}
		return nil
	})
}

type orderAlias struct{}

func TestPostgresFilteredCTEsAndCorrelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base, o := models.Orders.Query(), models.Orders
		common := rowset.AsRecord[orderAlias](rowset.CTE("eligible", rowset.ModelRecord(base.Where(orm.NumberGt(o.TotalCents, 1)))), "eligible_source")
		f := rowset.Scope(common)
		eligible := rowset.InQuery(expr.Value(orm.KeyExpr(o.ID)), rowset.SelectScalar(common, expr.Row(expr.At(f, orm.KeyExpr(models.Orders.ID))).Value()))
		if count, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(base), expr.Count[models.Order]().Filter(eligible).Value())).RequireFirst(t.Context(), tx); err != nil || count != 2 {
			t.Fatal("CTE dependency only in FILTER", count, err)
		}
		people := models.Users.Query()
		orders := rowset.As[orderAlias](base, "purchase")
		link := rowset.Correlate(rowset.From(people), orders)
		p, child := rowset.OuterScope(link, rowset.ModelScope(people)), rowset.InnerScope(link, rowset.Scope(orders))
		count := expr.Count[rowset.Correlation[models.User, rowset.Alias[orderAlias, models.Order]]]()
		filtered := rowset.SelectCorrelatedScalar(link, count.Filter(expr.Equal(expr.At(child, orm.ColumnExpr(models.Orders.BuyerID)), expr.At(p, orm.KeyExpr(models.Users.ID)))).Value())
		got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(people), rowset.CorrelatedScalarRowQuery(filtered).Value()), expr.Value(orm.TextExpr(models.Users.Email)).Asc())).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](1), value.Of[int64](0)}) {
			t.Fatal("correlated filter count", got, err)
		}
		// PostgreSQL would move this aggregate to the parent SELECT; reject it before execution.
		invalid := rowset.SelectCorrelatedScalar(link, count.Filter(expr.OrderRow(expr.At(p, orm.NumberExpr(models.Users.Age))).Gt(0)).Value())
		if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(people), rowset.CorrelatedScalarRowQuery(invalid).Value()))); !errors.Is(err, fault.Invalid) {
			t.Fatal("outer-only filter silently changed query ownership", err)
		}
		return nil
	})
}
