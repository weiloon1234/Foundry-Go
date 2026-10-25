package cursorqueries_test

import (
	"reflect"
	"slices"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGroupedAndJoinedCursors(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		o, totals := models.Orders, reports.BuyerTotalRows
		grouped := rows.Having(rows.GroupBy(rows.Select(rows.From(o.Query()), totals.Row,
			rows.Map(totals.BuyerID, expr.Value(o.BuyerID)), rows.MapSelected(totals.Total, expr.Sum(o.TotalCents).Value()), rows.MapSelected(totals.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(totals.Average, expr.Avg(o.TotalCents).Value())), expr.Group(expr.Value(o.BuyerID))), expr.Count[models.Order]().Gt(0))
		cursor := rows.CursorFor(rows.RecordOf(grouped))
		total, buyerID := rows.At(cursor.Scope(), totals.Total), rows.At(cursor.Scope(), totals.BuyerID)
		want, err := rows.OrderBy(grouped, expr.Sum(o.TotalCents).Desc(), expr.Value(o.BuyerID).Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, cursor.OrderBy(total.Desc()).UniqueBy(expr.Group(buyerID)), want)
		buyers := rows.As[buyerAlias](models.Users.Query(), "buyer")
		orders := rows.As[orderAlias](models.Orders.Query(), "purchase")
		joined := rows.InnerJoin(buyers, orders, rows.On(expr.At(rows.Scope(buyers), models.Users.Key()), expr.At(rows.Scope(orders), models.Orders.BuyerID)))
		left, right := rows.LeftScope(joined, rows.Scope(buyers)), rows.RightScope(joined, rows.Scope(orders))
		buyerIDInput, orderIDInput := expr.At(left, models.Users.Key()), expr.At(right, models.Orders.Key())
		pairRows := reports.OrderBuyerRows
		pairs := rows.Select(joined, pairRows.Row, rows.Map(pairRows.BuyerID, buyerIDInput), rows.Map(pairRows.OrderID, orderIDInput), rows.Map(pairRows.BuyerEmail, expr.At(left, models.Users.Email)), rows.Map(pairRows.TotalCents, expr.At(right, models.Orders.TotalCents)))
		pairCursor := rows.CursorFor(rows.RecordOf(pairs))
		pairBuyer, pairOrder := rows.At(pairCursor.Scope(), pairRows.BuyerID), rows.At(pairCursor.Scope(), pairRows.OrderID)
		pairWant, err := pairs.OrderBy(buyerIDInput.Asc(), orderIDInput.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, pairCursor.UniqueBy(expr.Group(pairBuyer), expr.Group(pairOrder)), pairWant)
		winnerPairs := rows.DistinctOn(pairs, expr.Group(buyerIDInput)).OrderBy(buyerIDInput.Asc(), orderIDInput.Desc())
		pairWinners, err := winnerPairs.All(t.Context(), tx)
		if err != nil {
			return err
		}
		// UUIDs generated within the same millisecond need not follow insertion
		// order. Derive each group's last ordered row from the complete baseline,
		// rather than assuming the two-order buyer sorts before the one-order buyer.
		var expectedWinners []reports.OrderBuyerRow
		for _, pair := range pairWant {
			last := len(expectedWinners) - 1
			if last >= 0 && expectedWinners[last].BuyerID == pair.BuyerID {
				expectedWinners[last] = pair
			} else {
				expectedWinners = append(expectedWinners, pair)
			}
		}
		if len(pairWinners) != 2 || !reflect.DeepEqual(pairWinners, expectedWinners) {
			t.Fatal("distinct query did not select the last ordered row per buyer")
		}
		// Exercise both group orders independently of whichever UUIDs were drawn.
		reversed, err := rows.DistinctOn(pairs, expr.Group(buyerIDInput)).OrderBy(buyerIDInput.Desc(), orderIDInput.Desc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		slices.Reverse(expectedWinners)
		if !reflect.DeepEqual(reversed, expectedWinners) {
			t.Fatal("descending groups changed the selected winner")
		}
		winnerPairsCursor := rows.CursorFor(rows.RecordOf(winnerPairs))
		winnerBuyer := rows.At(winnerPairsCursor.Scope(), pairRows.BuyerID)
		checkTraversal(t, tx, winnerPairsCursor.UniqueBy(expr.Group(winnerBuyer)), pairWinners)
		return nil
	})
}
func TestPostgresScalarCursorNullEndpointsAndWindows(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		values := rows.DistinctScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Nickname))))
		for _, source := range []rows.ValueSource[value.Nullable[string]]{rows.Values(values), rows.UnionValues(rows.Values(values), rows.Values(values))} {
			c := rows.ValueCursorFor(source)
			checkTraversal(t, tx, c.OrderBy(c.Value().Asc()).UniqueBy(c.Key()), []value.Nullable[string]{value.Of("Bee"), {}})
			checkTraversal(t, tx, c.OrderBy(c.Value().Desc()).UniqueBy(c.Key()), []value.Nullable[string]{{}, value.Of("Bee")})
			q := c.UniqueBy(c.Key())
			first, err := q.Paginate(t.Context(), tx, orm.CursorRequest[value.Nullable[string]]{Size: 1})
			if err != nil {
				return err
			}
			last, err := q.Paginate(t.Context(), tx, orm.CursorRequest[value.Nullable[string]]{Size: 1, After: first.Next})
			if err != nil {
				return err
			}
			// Before Bee under ASC is empty; after NULL under ASC is also empty.
			empty, err := q.Paginate(t.Context(), tx, orm.CursorRequest[value.Nullable[string]]{Size: 1, After: last.Previous})
			if err != nil || len(empty.Items) != 0 || empty.Next.IsSet() || empty.Previous.IsSet() {
				t.Fatal("after NULL was not empty", err)
			}
			beforeBee, err := q.Paginate(t.Context(), tx, orm.CursorRequest[value.Nullable[string]]{Size: 1, Before: first.Next})
			if err != nil || len(beforeBee.Items) != 0 || beforeBee.Next.IsSet() || beforeBee.Previous.IsSet() {
				t.Fatal("before first was not empty", err)
			}
		}
		window := expr.WindowFor(rows.ModelScope(models.Users.Query())).OrderBy(expr.Value(models.Users.Email).Asc())
		numbers := rows.SelectScalar(rows.From(models.Users.Query()), expr.RowNumber(window))
		c := rows.ValueCursorFor(rows.Values(numbers))
		checkTraversal(t, tx, c.UniqueBy(c.Key()), []int64{1, 2, 3})
		return nil
	})
}
