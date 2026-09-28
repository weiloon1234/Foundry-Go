package cursorqueries_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func summaries() query.ProjectionQuery[models.User, reports.UserSummary] {
	u := models.UserFields()
	return reports.SelectUserSummary(models.QueryUsers(), reports.UserSummarySelection[models.User]{ID: u.ID.Value(), Email: u.Email.Value(), Nickname: u.Nickname.Value(), Status: u.Status.Value()})
}

// Traverse both directions, including an empty page after the terminal row.
// Comparing whole records catches decoder, NULL and exact-value regressions.
func checkTraversal[R any](t *testing.T, tx *database.Tx, q query.CursorQuery[R], want []R) {
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

func TestPostgresProjectionCursors(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		cursor := query.CursorFor(summaries())
		cursorFields := reports.UserSummaryFieldsAt(cursor.Scope())
		ordered := cursor.OrderBy(cursorFields.Nickname.Asc(), cursorFields.Email.Desc()).UniqueBy(cursorFields.ID.Group())
		want, err := summaries().OrderBy(models.UserFields().Nickname.Asc(), models.UserFields().Email.Desc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, ordered, want)
		first, err := ordered.Paginate(t.Context(), tx, query.CursorRequest[reports.UserSummary]{Size: 1})
		if err != nil {
			return err
		}
		for _, changed := range []query.CursorQuery[reports.UserSummary]{
			ordered.Where(cursorFields.Email.Ne(users[0].Email)),
			cursor.OrderBy(cursorFields.Nickname.Desc()).UniqueBy(cursorFields.ID.Group()),
			cursor.OrderBy(cursorFields.Nickname.Asc(), cursorFields.Email.Desc()).UniqueBy(cursorFields.ID.Group(), cursorFields.Email.Group()),
		} {
			page, err := changed.Paginate(t.Context(), queryfixture.NoQueries(t), query.CursorRequest[reports.UserSummary]{Size: 1, After: first.Next})
			if !errors.Is(err, fault.Invalid) || page.Items != nil {
				t.Fatal("cross-query cursor accepted", err)
			}
		}
		// Input ordering/windows identify a bounded dataset independently of
		// the order used to traverse it. No inner limit/offset is discarded.
		input := summaries().OrderBy(models.UserFields().Email.Asc()).Limit(2).Offset(1)
		bounded := query.CursorFor(input)
		bf := reports.UserSummaryFieldsAt(bounded.Scope())
		boundedWant, err := input.All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, bounded.UniqueBy(bf.Email.Group()), boundedWant)
		return nil
	})
}

type reportAlias struct{}
type buyerAlias struct{}
type orderAlias struct{}

func TestPostgresRecordCursorMetadataSurvivesComposition(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		input := summaries()
		cte := query.CTE("foundry_cursor", input)
		a := query.As[reportAlias](cte, "summary")
		combined := input.Union(input)
		want, err := input.OrderBy(models.UserFields().Email.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for _, source := range []query.RecordQuerySource[reports.UserSummary]{cte, query.SelectRecord(a, a.Scope()), combined} {
			cursor := query.CursorFor(source)
			f := reports.UserSummaryFieldsAt(cursor.Scope())
			checkTraversal(t, tx, cursor.OrderBy(f.Email.Asc()).UniqueBy(f.ID.Group()), want)
		}
		users := models.QueryUsers()
		for _, source := range []query.RecordQuerySource[models.User]{users, query.SelectRecord(users, users.Scope()), query.CTE("cursor_users", users), users.Union(users)} {
			cursor := query.CursorFor(source)
			f := models.UserFieldsAt(cursor.Scope())
			want, err := users.OrderBy(models.UserFields().Email.Asc()).All(t.Context(), tx)
			if err != nil {
				return err
			}
			checkTraversal(t, tx, cursor.OrderBy(f.Email.Asc()).UniqueBy(f.ID.Group()), want)
		}
		return nil
	})
}

func TestPostgresRecursiveResultCursor(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.UserFields()
		tree := query.RecursiveCTE("cursor_tree", models.QueryUsers().Where(u.ID.Eq(users[0].ID)), func(self query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
			parent := query.As[buyerAlias](self, "parent")
			child := query.As[orderAlias](models.QueryUsers(), "child")
			joined := query.InnerJoin(child, parent, query.On(models.UserFieldsAt(child.Scope()).IntroducerID, models.UserFieldsAt(parent.Scope()).ID))
			return query.SelectRecord(joined, query.LeftScope(joined, child.Scope()))
		})
		cursor := query.CursorFor(tree)
		f := models.UserFieldsAt(cursor.Scope())
		checkTraversal(t, tx, cursor.OrderBy(f.Email.Asc()).UniqueBy(f.ID.Group()), users[:2])
		return nil
	})
}

func TestPostgresGroupedAndJoinedCursors(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		o := models.OrderFields()
		grouped := reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{BuyerID: o.BuyerID.Value(), Total: o.TotalCents.Sum().Value(), Orders: query.Count[models.Order]().Value(), Average: o.TotalCents.Avg().Value()}).GroupBy(o.BuyerID.Group()).Having(query.Count[models.Order]().Gt(0))
		cursor := query.CursorFor(grouped)
		f := reports.BuyerTotalsFieldsAt(cursor.Scope())
		want, err := grouped.OrderBy(o.TotalCents.Sum().Desc(), o.BuyerID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, cursor.OrderBy(f.Total.Desc()).UniqueBy(f.BuyerID.Group()), want)
		buyers := query.As[buyerAlias](models.QueryUsers(), "buyer")
		orders := query.As[orderAlias](models.QueryOrders(), "purchase")
		joined := query.InnerJoin(buyers, orders, query.On(models.UserFieldsAt(buyers.Scope()).ID, models.OrderFieldsAt(orders.Scope()).BuyerID))
		left, right := query.LeftScope(joined, buyers.Scope()), query.RightScope(joined, orders.Scope())
		bf, of := models.UserFieldsAt(left), models.OrderFieldsAt(right)
		pairs := reports.ProjectOrderBuyerRow(joined).SelectBuyerID(bf.ID.Value()).SelectOrderID(of.ID.Value()).SelectBuyerEmail(bf.Email.Value()).SelectTotalCents(of.TotalCents.Value()).Query()
		pairCursor := query.CursorFor(pairs)
		pf := reports.OrderBuyerRowFieldsAt(pairCursor.Scope())
		pairWant, err := pairs.OrderBy(bf.ID.Asc(), of.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, pairCursor.UniqueBy(pf.BuyerID.Group(), pf.OrderID.Group()), pairWant)
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
		checkTraversal(t, tx, winnerPairsCursor.UniqueBy(wpf.BuyerID.Group()), pairWinners)
		winners := query.SelectRecord(joined, left).DistinctOn(bf.ID.Group()).OrderBy(bf.ID.Asc(), of.ID.Desc())
		winnerCursor := query.CursorFor(winners)
		wf := models.UserFieldsAt(winnerCursor.Scope())
		winnerWant, err := winners.All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, winnerCursor.UniqueBy(wf.ID.Group()), winnerWant)
		return nil
	})
}

func TestPostgresScalarCursorNullEndpointsAndWindows(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		values := query.SelectValue(models.QueryUsers(), models.UserFields().Nickname.Value()).Distinct()
		for _, source := range []query.ValueQuerySource[value.Nullable[string]]{values, values.Union(values)} {
			c := query.ValueCursorFor(source)
			checkTraversal(t, tx, c.OrderBy(c.Value().Asc()).UniqueBy(c.Key()), []value.Nullable[string]{value.Of("Bee"), {}})
			checkTraversal(t, tx, c.OrderBy(c.Value().Desc()).UniqueBy(c.Key()), []value.Nullable[string]{{}, value.Of("Bee")})
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
		checkTraversal(t, tx, c.UniqueBy(c.Key()), []int64{1, 2, 3})
		return nil
	})
}

func TestCursorValidationBeforeExecution(t *testing.T) {
	cursor := query.CursorFor(summaries())
	f := reports.UserSummaryFieldsAt(cursor.Scope())
	q := cursor.UniqueBy(f.ID.Group())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.Paginate(ctx, queryfixture.NoQueries(t), query.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, bad := range []query.CursorQuery[reports.UserSummary]{cursor, cursor.UniqueBy(f.ID.Group(), f.ID.Group()), q.OrderBy(f.ID.Asc(), f.ID.Desc()), {}} {
		if p, err := bad.Paginate(t.Context(), queryfixture.NoQueries(t), query.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, fault.Invalid) || p.Items != nil {
			t.Fatal("invalid cursor reached execution", err)
		}
	}
	for _, request := range []query.CursorRequest[reports.UserSummary]{{}, {Size: query.MaxPageSize + 1}, {Size: 1, After: value.Set(query.Cursor[reports.UserSummary]{}), Before: value.Set(query.Cursor[reports.UserSummary]{})}} {
		if _, err := q.Paginate(t.Context(), queryfixture.NoQueries(t), request); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestPostgresCursorFailureDiscardsPage(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		c := query.CursorFor(summaries())
		f := reports.UserSummaryFieldsAt(c.Scope())
		q := c.UniqueBy(f.Email.Group())
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE id = $1`, users[1].ID.String()); err != nil {
			return err
		}
		if page, err := q.Paginate(t.Context(), tx, query.CursorRequest[reports.UserSummary]{Size: 1}); err == nil || !reflect.DeepEqual(page, query.CursorPage[reports.UserSummary]{}) {
			t.Fatal("invalid lookahead leaked page", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'active', email_address = $1 WHERE id = $2`, strings.Repeat("x", query.MaxCursorBytes+1), users[1].ID.String()); err != nil {
			return err
		}
		if page, err := c.OrderBy(f.Email.Desc()).UniqueBy(f.ID.Group()).Paginate(t.Context(), tx, query.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(page, query.CursorPage[reports.UserSummary]{}) {
			t.Fatal("oversized token leaked page", err)
		}
		if _, err := models.QueryUsers().Count(t.Context(), tx); err != nil {
			return err
		}
		return nil
	})
}
