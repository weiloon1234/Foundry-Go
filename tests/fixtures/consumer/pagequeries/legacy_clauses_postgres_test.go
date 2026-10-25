package pagequeries_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"testing"
)

func legacySummaries() query.ProjectionQuery[models.User, reports.UserSummary] {
	u := models.UserFields()
	return reports.SelectUserSummary(models.QueryUsers(), reports.UserSummarySelection[models.User]{ID: query.Value(u.ID), Email: query.Value(u.Email), Nickname: query.Value(u.Nickname), Status: query.Value(u.Status)}).OrderBy(u.Email.Asc(), u.ID.Asc())
}
func TestPostgresPagesRetainRelationalSemantics(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		grouped := reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{BuyerID: query.Value(o.BuyerID), Total: query.Sum(o.TotalCents).Value(), Orders: query.Count[models.Order]().Value(), Average: query.Avg(o.TotalCents).Value()}).GroupBy(o.BuyerID.Group()).Having(query.Count[models.Order]().Gt(0)).OrderBy(o.BuyerID.Asc())
		p, err := grouped.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		if p.Total != 2 || p.Pages != 2 || len(p.Items) != 1 {
			t.Fatal("pagination counted input rows instead of groups")
		}
		v, valid := p.Items[0].Total.Get()
		if !valid || v.String() != "3" {
			t.Fatal("page lost exact aggregate codec")
		}
		definition := query.CTE("paged_reports", legacySummaries().Limit(2))
		a := query.As[reportAlias](definition, "report")
		f := reports.UserSummaryFieldsAt(a.Scope())
		derived := query.SelectRecord(a, a.Scope()).OrderBy(f.ID.Asc())
		d, err := derived.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil || d.Total != 2 {
			t.Fatal("page discarded nested CTE limit", err)
		}
		combined := legacySummaries().Limit(1).UnionAll(legacySummaries().Limit(2))
		sf := reports.UserSummaryFieldsAt(combined.Scope())
		combined = combined.OrderBy(sf.Email.Asc())
		setPage, err := combined.Paginate(t.Context(), tx, query.PageRequest{Number: 2, Size: 2})
		if err != nil || setPage.Total != 3 || len(setPage.Items) != 1 {
			t.Fatal("set page lost multiplicity/input limits", err)
		}
		if simple, err := combined.SimplePaginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 2}); err != nil || !simple.HasMore {
			t.Fatal("set simple page failed", err)
		}
		values := query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Nickname)).Distinct().OrderBy(models.UserFields().Nickname.Asc())
		vp, err := values.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil || vp.Total != 2 || len(vp.Items) != 1 || vp.Items[0] != value.Of("Bee") {
			t.Fatal("distinct/nullable value page failed", err)
		}
		vs := values.Union(values)
		vs = vs.OrderBy(vs.Value().Asc())
		if p, err := vs.SimplePaginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err != nil || !p.HasMore {
			t.Fatal("combined value simple page failed", err)
		}
		if p, err := vs.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err != nil || p.Total != 2 {
			t.Fatal("combined value page failed", err)
		}
		window := query.WindowFor(models.QueryUsers()).OrderBy(models.UserFields().Email.Asc())
		position := query.RowNumber(window)
		if p, err := query.SelectValue(models.QueryUsers(), position).OrderBy(position.Asc()).Paginate(t.Context(), tx, query.PageRequest{Number: 2, Size: 1}); err != nil || p.Total != 3 || !reflect.DeepEqual(p.Items, []int64{2}) {
			t.Fatal("window page evaluated after limit", err)
		}
		buyers := query.As[buyerAlias](models.QueryUsers(), "buyer")
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		joined := query.InnerJoin(buyers, orders, query.On(models.UserFieldsAt(buyers.Scope()).ID, models.OrderFieldsAt(orders.Scope()).BuyerID))
		left := query.LeftScope(joined, buyers.Scope())
		right := query.RightScope(joined, orders.Scope())
		jf := models.UserFieldsAt(left)
		of := models.OrderFieldsAt(right)
		jq := query.SelectRecord(joined, left).OrderBy(jf.Email.Asc(), of.ID.Asc())
		if p, err := jq.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err != nil || p.Total != 3 || p.Items[0].ID != users[0].ID {
			t.Fatal("join page lost multiplicity", err)
		}
		winners := query.SelectRecord(joined, left).DistinctOn(jf.ID.Group()).OrderBy(jf.ID.Asc(), of.ID.Desc())
		if p, err := winners.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err != nil || p.Total != 2 {
			t.Fatal("distinct-on page lost winner ordering", err)
		}
		return nil
	})
}
