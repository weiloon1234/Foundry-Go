package inner_test

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type referralAlias struct{}
type purchaseAlias struct{}

func TestPostgresInnerJoinsAndGrouping(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		referrals := rows.As[referralAlias](models.Users.Query(), "referral")
		r := rows.Scope(referrals)
		// Inner joins preserve non-nullable fields and support aggregates in the joined scope.
		purchases := rows.As[purchaseAlias](models.Orders.Query(), "purchase")
		p := rows.Scope(purchases)
		inner := rows.InnerJoin(purchases, referrals, rows.OnEqual(expr.At(p, models.Orders.BuyerID), expr.At(r, models.Users.ID)))
		order := rows.LeftScope(inner, rows.Scope(purchases))
		buyer := rows.RightScope(inner, rows.Scope(referrals))
		orderReport := rows.OrderBy(rows.Select(inner, reports.OrderBuyerRows,
			rows.Map(reports.OrderBuyerRows.OrderID, expr.At(order, models.Orders.ID)),
			rows.Map(reports.OrderBuyerRows.BuyerID, expr.At(buyer, models.Users.ID)),
			rows.Map(reports.OrderBuyerRows.BuyerEmail, expr.At(buyer, models.Users.Email)),
			rows.Map(reports.OrderBuyerRows.TotalCents, expr.At(order, models.Orders.TotalCents))), expr.At(order, models.Orders.TotalCents).Asc())
		orders, err := rows.Read(orderReport).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(orders) != 3 || orders[2].BuyerID != users[1].ID || orders[0].BuyerEmail != users[0].Email {
			t.Error("inner joined projection decoded the wrong source")
		}
		total := expr.SumAt(order, models.Orders.TotalCents)
		count := expr.CountFieldAt(order, models.Orders.ID)
		totals := rows.OrderBy(rows.Having(rows.GroupBy(rows.Select(inner, reports.BuyerTotalRows,
			rows.Map(reports.BuyerTotalRows.BuyerID, expr.At(buyer, models.Users.ID)),
			rows.MapSelected(reports.BuyerTotalRows.Total, total.Value()),
			rows.MapSelected(reports.BuyerTotalRows.Orders, count.Value()),
			rows.MapSelected(reports.BuyerTotalRows.Average, expr.AvgAt(order, models.Orders.TotalCents).Value())), expr.Group(expr.At(buyer, models.Users.ID))), count.Gt(1)), total.Desc())
		groups, err := rows.Read(totals).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(groups) != 1 || groups[0].Orders != 2 || groups[0].BuyerID != users[0].ID {
			t.Error("joined grouping/HAVING failed")
		}
		return nil
	})
}
