package cursorqueries_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"slices"
	"testing"
)

func TestPostgresGroupedAndJoinedCursors(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		o := models.OrderFields()
		grouped := reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{BuyerID: query.Value(o.BuyerID), Total: query.Sum(o.TotalCents).Value(), Orders: query.Count[models.Order]().Value(), Average: query.Avg(o.TotalCents).Value()}).GroupBy(o.BuyerID.Group()).Having(query.Count[models.Order]().Gt(0))
		cursor := query.CursorFor(grouped)
		f := reports.BuyerTotalsFieldsAt(cursor.Scope())
		want, err := grouped.OrderBy(query.Sum(o.TotalCents).Desc(), o.BuyerID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkLegacyTraversal(t, tx, cursor.OrderBy(f.Total.Desc()).UniqueBy(f.BuyerID.Group()), want)
		buyers := query.As[buyerAlias](models.QueryUsers(), "buyer")
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		joined := query.InnerJoin(buyers, orders, query.On(models.UserFieldsAt(buyers.Scope()).ID, models.OrderFieldsAt(orders.Scope()).BuyerID))
		left, right := query.LeftScope(joined, buyers.Scope()), query.RightScope(joined, orders.Scope())
		bf, of := models.UserFieldsAt(left), models.OrderFieldsAt(right)
		pairs := reports.ProjectOrderBuyerRow(joined).SelectBuyerID(query.Value(bf.ID)).SelectOrderID(query.Value(of.ID)).SelectBuyerEmail(query.Value(bf.Email)).SelectTotalCents(query.Value(of.TotalCents)).Query()
		pairCursor := query.CursorFor(pairs)
		pf := reports.OrderBuyerRowFieldsAt(pairCursor.Scope())
		pairWant, err := pairs.OrderBy(bf.ID.Asc(), of.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkLegacyTraversal(t, tx, pairCursor.UniqueBy(pf.BuyerID.Group(), pf.OrderID.Group()), pairWant)
		winnerPairs := pairs.DistinctOn(bf.ID.Group()).OrderBy(bf.ID.Asc(), of.ID.Desc())
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
		reversed, err := pairs.DistinctOn(bf.ID.Group()).OrderBy(bf.ID.Desc(), of.ID.Desc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		slices.Reverse(expectedWinners)
		if !reflect.DeepEqual(reversed, expectedWinners) {
			t.Fatal("descending groups changed the selected winner")
		}
		winnerPairsCursor := query.CursorFor(winnerPairs)
		wpf := reports.OrderBuyerRowFieldsAt(winnerPairsCursor.Scope())
		checkLegacyTraversal(t, tx, winnerPairsCursor.UniqueBy(wpf.BuyerID.Group()), pairWinners)
		winners := query.SelectRecord(joined, left).DistinctOn(bf.ID.Group()).OrderBy(bf.ID.Asc(), of.ID.Desc())
		winnerCursor := query.CursorFor(winners)
		wf := models.UserFieldsAt(winnerCursor.Scope())
		winnerWant, err := winners.All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkLegacyTraversal(t, tx, winnerCursor.UniqueBy(wf.ID.Group()), winnerWant)
		return nil
	})
}
func TestPostgresScalarCursorNullEndpointsAndWindows(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		values := query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Nickname)).Distinct()
		for _, source := range []query.ValueQuerySource[value.Nullable[string]]{values, values.Union(values)} {
			c := query.ValueCursorFor(source)
			checkLegacyTraversal(t, tx, c.OrderBy(c.Value().Asc()).UniqueBy(c.Key()), []value.Nullable[string]{value.Of("Bee"), {}})
			checkLegacyTraversal(t, tx, c.OrderBy(c.Value().Desc()).UniqueBy(c.Key()), []value.Nullable[string]{{}, value.Of("Bee")})
			q := c.UniqueBy(c.Key())
			first, err := q.Paginate(t.Context(), tx, query.CursorRequest[value.Nullable[string]]{Size: 1})
			if err != nil {
				return err
			}
			last, err := q.Paginate(t.Context(), tx, query.CursorRequest[value.Nullable[string]]{Size: 1, After: first.Next})
			if err != nil {
				return err
			}
			// Before Bee under ASC is empty; after NULL under ASC is also empty.
			empty, err := q.Paginate(t.Context(), tx, query.CursorRequest[value.Nullable[string]]{Size: 1, After: last.Previous})
			if err != nil || len(empty.Items) != 0 || empty.Next.IsSet() || empty.Previous.IsSet() {
				t.Fatal("after NULL was not empty", err)
			}
			beforeBee, err := q.Paginate(t.Context(), tx, query.CursorRequest[value.Nullable[string]]{Size: 1, Before: first.Next})
			if err != nil || len(beforeBee.Items) != 0 || beforeBee.Next.IsSet() || beforeBee.Previous.IsSet() {
				t.Fatal("before first was not empty", err)
			}
		}
		window := query.WindowFor(models.QueryUsers()).OrderBy(models.UserFields().Email.Asc())
		numbers := query.SelectValue(models.QueryUsers(), query.RowNumber(window))
		c := query.ValueCursorFor(numbers)
		checkLegacyTraversal(t, tx, c.UniqueBy(c.Key()), []int64{1, 2, 3})
		return nil
	})
}
func checkLegacyTraversal[R any](t *testing.T, tx *database.Tx, q query.CursorQuery[R], want []R) {
	t.Helper()
	counter := &queryfixture.QueryCounter{Executor: tx}
	request := query.CursorRequest[R]{Size: 1}
	var last query.CursorPage[R]
	for i := range want {
		before := counter.Queries.Load()
		page, err := q.Paginate(t.Context(), counter, request)
		if err != nil {
			t.Fatal(err)
		}
		if counter.Queries.Load() != before+1 || !reflect.DeepEqual(page.Items, want[i:i+1]) || page.Size != 1 || page.Next.IsSet() != (i+1 < len(want)) || page.Previous.IsSet() != (i > 0) {
			t.Fatalf("forward page %d: %+v, want %+v", i, page, want[i:i+1])
		}
		last = page
		request.After = page.Next
	}
	for i := len(want) - 2; i >= 0; i-- {
		page, err := q.Paginate(t.Context(), counter, query.CursorRequest[R]{Size: 1, Before: last.Previous})
		if err != nil || !reflect.DeepEqual(page.Items, want[i:i+1]) || page.Previous.IsSet() != (i > 0) || !page.Next.IsSet() {
			t.Fatalf("backward page %d: %+v, %v", i, page, err)
		}
		last = page
	}
	if len(want) > 1 {
		// Changing page size retains the query identity. A backward read gives
		// us a Next token for the last row even when the forward page had none.
		tail, err := q.Paginate(t.Context(), tx, query.CursorRequest[R]{Size: len(want), After: last.Next})
		if err != nil || !reflect.DeepEqual(tail.Items, want[1:]) {
			t.Fatalf("changed size: %+v, %v", tail, err)
		}
	}
}
