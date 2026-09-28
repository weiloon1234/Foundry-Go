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
		orderReport := reports.ProjectOrderBuyerRow(inner).SelectOrderID(order.ID.Value()).SelectBuyerID(buyer.ID.Value()).
			SelectBuyerEmail(buyer.Email.Value()).SelectTotalCents(order.TotalCents.Value()).Query().OrderBy(order.TotalCents.Asc())
		orders, err := orderReport.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(orders) != 3 || orders[2].BuyerID != users[1].ID || orders[0].BuyerEmail != users[0].Email {
			t.Error("inner joined projection decoded the wrong source")
		}
		totals := reports.ProjectBuyerTotals(inner).SelectBuyerID(buyer.ID.Value()).SelectTotal(order.TotalCents.Sum().Value()).
			SelectOrders(order.ID.Count().Value()).SelectAverage(order.TotalCents.Avg().Value()).Query().
			GroupBy(buyer.ID.Group()).Having(order.ID.Count().Gt(1)).OrderBy(order.TotalCents.Sum().Desc())
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
