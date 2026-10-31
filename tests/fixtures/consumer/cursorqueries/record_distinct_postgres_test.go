package cursorqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// Complete records preserve input-scope DISTINCT ON ordering through pagination.
func TestPostgresCompleteRecordDistinctOnCursor(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		buyerSource := rows.As[buyerAlias](models.Users.Query(), "buyer")
		orderSource := rows.As[orderAlias](models.Orders.Query(), "purchase")
		joinedSource := rows.InnerJoin(buyerSource, orderSource, rows.On(expr.At(rows.Scope(buyerSource), orm.KeyExpr(models.Users.ID)), expr.At(rows.Scope(orderSource), orm.ColumnExpr(models.Orders.BuyerID))))
		buyerScope := rows.LeftScope(joinedSource, rows.Scope(buyerSource))
		orderScope := rows.RightScope(joinedSource, rows.Scope(orderSource))
		buyerID, orderID := expr.At(buyerScope, orm.KeyExpr(models.Users.ID)), expr.At(orderScope, orm.KeyExpr(models.Orders.ID))
		winners := rows.DistinctOnRecord(rows.SelectRecord(joinedSource, buyerScope), expr.Group(buyerID)).OrderBy(expr.Asc(buyerID), expr.Desc(orderID))
		winnerCursor := rows.CursorFor(winners)
		wf := expr.At(winnerCursor.Scope(), orm.KeyExpr(models.Users.ID))
		winnerWant, err := rows.ReadRecord(winners).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, winnerCursor.UniqueBy(expr.Group(wf)), winnerWant)
		return nil
	})
}
