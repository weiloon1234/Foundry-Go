package inner_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"testing"
)

type referralAlias struct{}
type purchaseAlias struct{}

func TestPostgresInnerJoinsAndGrouping(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := query.As[referralAlias](models.QueryUsers(), "referral")
		r := models.UserFieldsAt(referrals.Scope())
		// Inner joins preserve non-nullable fields and support aggregates in the joined scope.
		purchases := query.As[purchaseAlias](models.QueryOrders(), "purchase")
		p := models.OrderFieldsAt(purchases.Scope())
		inner := query.InnerJoin(purchases, referrals, query.On(p.BuyerID, r.ID))
		order := models.OrderFieldsAt(query.LeftScope(inner, purchases.Scope()))
		buyer := models.UserFieldsAt(query.RightScope(inner, referrals.Scope()))
		orderReport := reports.ProjectOrderBuyerRow(inner).SelectOrderID(query.Value(order.ID)).SelectBuyerID(query.Value(buyer.ID)).
			SelectBuyerEmail(query.Value(buyer.Email)).SelectTotalCents(query.Value(order.TotalCents)).Query().OrderBy(order.TotalCents.Asc())
		orders, err := orderReport.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(orders) != 3 || orders[2].BuyerID != users[1].ID || orders[0].BuyerEmail != users[0].Email {
			t.Error("inner joined projection decoded the wrong source")
		}
		totals := reports.ProjectBuyerTotals(inner).SelectBuyerID(query.Value(buyer.ID)).SelectTotal(query.Sum(order.TotalCents).Value()).
			SelectOrders(query.CountField(order.ID).Value()).SelectAverage(query.Avg(order.TotalCents).Value()).Query().
			GroupBy(buyer.ID.Group()).Having(query.CountField(order.ID).Gt(1)).OrderBy(query.Sum(order.TotalCents).Desc())
		groups, err := totals.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0].Orders != 2 || groups[0].BuyerID != users[0].ID {
			t.Error("joined grouping/HAVING failed")
		}
		return nil
	})
}
