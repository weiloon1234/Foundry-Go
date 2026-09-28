package filterqueries_test

import (
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresFilteredGroupsAndWindows(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		base := models.QueryOrders()
		filtered := o.TotalCents.Sum().Filter(o.TotalCents.Gt(1))
		q := reports.SelectBuyerTotals(base, reports.BuyerTotalsSelection[models.Order]{
			BuyerID: o.BuyerID.Value(), Total: filtered.Value(),
			Orders:  query.Count[models.Order]().Value(),
			Average: o.TotalCents.Avg().Filter(o.TotalCents.Lt(3)).Value(),
		}).GroupBy(o.BuyerID.Group()).Having(filtered.Gt(decimal.FromInt64(1)))
		rows, err := q.All(t.Context(), tx)
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
		w := query.WindowFor(base).OrderBy(o.TotalCents.Asc()).RowsBetween(query.UnboundedPreceding(), query.CurrentRow())
		window := windowqueries.SelectWindowTotal(base, windowqueries.WindowTotalSelection[models.Order]{
			Amount: o.TotalCents.Value(), Running: filtered.Over(w),
			Count: query.Count[models.Order]().Filter(o.TotalCents.Gt(1)).Over(w),
			Any:   query.Exists[models.Order]().Filter(o.TotalCents.Gt(2)).Over(w),
		}).OrderBy(o.TotalCents.Asc())
		values, err := window.All(t.Context(), tx)
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
		grouped := query.Count[models.Order]().Filter(o.BuyerID.Eq(users[0].ID)).Over(query.WindowFor(base))
		if got, err := query.SelectValue(base, grouped).GroupBy(o.BuyerID.Group()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("grouped filtered window", got, err)
		}
		return nil
	})
}

func TestPostgresFilteredNullsAndRelations(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.UserFields(), models.OrderFields()
		people := models.QueryUsers()
		for name, computation := range map[string]query.OrderedAggregate[models.User, int64]{
			"non-null": u.Nickname.Count().Filter(u.Nickname.Like("B%")),
			"distinct": u.Nickname.CountDistinct().Filter(u.Age.Gt(0)),
			"unknown":  query.Count[models.User]().Filter(u.Nickname.Like("Z%")),
		} {
			got, err := query.SelectValue(people, computation.Value()).RequireFirst(t.Context(), tx)
			want := int64(1)
			if name == "unknown" {
				want = 0
			}
			if err != nil || got != want {
				t.Fatal(name, got, err)
			}
		}
		if got, err := query.SelectValue(people, u.Age.Min().Filter(u.Age.Lt(0)).Value()).RequireFirst(t.Context(), tx); err != nil || !got.IsNull() {
			t.Fatal("filtered empty extrema", got, err)
		}
		if got, err := query.SelectValue(people, query.Exists[models.User]().Filter(u.Age.Lt(0)).Value()).RequireFirst(t.Context(), tx); err != nil || got {
			t.Fatal("filtered empty existence", err)
		}
		aggregates, relations := models.UserAggregates(), models.UserRelations()
		count := aggregates.OrderCount.Using(query.Related(relations.Orders, query.Count[models.Order]().Filter(o.TotalCents.Gt(2))))
		total := aggregates.OrderTotal.Using(query.Related(relations.Orders, o.TotalCents.Sum().Filter(o.TotalCents.Gt(2))))
		rows, err := people.With(count, total).OrderBy(u.Email.Asc()).All(t.Context(), tx)
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
		base, o := models.QueryOrders(), models.OrderFields()
		common := query.As[orderAlias](query.CTE("eligible", base.Where(o.TotalCents.Gt(1))), "eligible_source")
		f := models.OrderFieldsAt(common.Scope())
		eligible := o.ID.InQuery(query.SelectValue(common, f.ID.Value()))
		if count, err := query.SelectValue(base, query.Count[models.Order]().Filter(eligible).Value()).RequireFirst(t.Context(), tx); err != nil || count != 2 {
			t.Fatal("CTE dependency only in FILTER", count, err)
		}
		people := models.QueryUsers()
		orders := query.As[orderAlias](base, "purchase")
		link := query.Correlate(people, orders)
		p, child := models.UserFieldsAt(query.OuterScope(link, people.Scope())), models.OrderFieldsAt(query.InnerScope(link, orders.Scope()))
		count := query.Count[query.Correlation[models.User, query.Alias[orderAlias, models.Order]]]()
		filtered := query.SelectCorrelatedValue(link, count.Filter(child.BuyerID.EqColumn(p.ID)).Value())
		got, err := query.SelectValue(people, query.CorrelatedScalarQuery(filtered)).OrderBy(models.UserFields().Email.Asc()).All(t.Context(), tx)
		if err != nil || !reflect.DeepEqual(got, []value.Nullable[int64]{value.Of[int64](2), value.Of[int64](1), value.Of[int64](0)}) {
			t.Fatal("correlated filter count", got, err)
		}
		// PostgreSQL would move this aggregate to the parent SELECT; reject it before execution.
		invalid := query.SelectCorrelatedValue(link, count.Filter(p.Age.Gt(0)).Value())
		if _, err := query.SelectValue(people, query.CorrelatedScalarQuery(invalid)).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("outer-only filter silently changed query ownership", err)
		}
		return nil
	})
}
