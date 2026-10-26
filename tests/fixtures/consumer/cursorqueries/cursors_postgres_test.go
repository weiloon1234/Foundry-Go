package cursorqueries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func summaries() rows.Query[models.User, reports.UserSummary] {
	u, r := models.Users, reports.UserSummaries
	return rows.Select(rows.From(u.Query()), r.Row, rows.MapSelected(r.ID, expr.Row(expr.Value(u.Key())).Value()), rows.MapSelected(r.Email, expr.Row(expr.Value(u.Email)).Value()), rows.MapSelected(r.Nickname, expr.Row(expr.Value(u.Nickname)).Value()), rows.MapSelected(r.Status, expr.Row(expr.Value(u.Status)).Value()))
}

// Traverse both directions, including an empty page after the terminal row.
// Comparing whole records catches decoder, NULL and exact-value regressions.
func checkTraversal[R any](t *testing.T, tx *database.Tx, q interface {
	Paginate(context.Context, database.Executor, orm.CursorRequest[R]) (orm.CursorPage[R], error)
}, want []R) {
	t.Helper()
	counter := &queryfixture.QueryCounter{Executor: tx}
	request := orm.CursorRequest[R]{Size: 1}
	var last orm.CursorPage[R]
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
		page, err := q.Paginate(t.Context(), counter, orm.CursorRequest[R]{Size: 1, Before: last.Previous})
		if err != nil || !reflect.DeepEqual(page.Items, want[i:i+1]) || page.Previous.IsSet() != (i > 0) || !page.Next.IsSet() {
			t.Fatalf("backward page %d: %+v, %v", i, page, err)
		}
		last = page
	}
	if len(want) > 1 {
		// Changing page size retains the query identity. A backward read gives
		// us a Next token for the last row even when the forward page had none.
		tail, err := q.Paginate(t.Context(), tx, orm.CursorRequest[R]{Size: len(want), After: last.Next})
		if err != nil || !reflect.DeepEqual(tail.Items, want[1:]) {
			t.Fatalf("changed size: %+v, %v", tail, err)
		}
	}
}

func TestPostgresProjectionCursors(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		cursor := rows.CursorFor(rows.RecordOf(summaries()))
		ordered := cursor.OrderBy(rows.At(cursor.Scope(), reports.UserSummaries.Nickname).Asc(), rows.At(cursor.Scope(), reports.UserSummaries.Email).Desc()).UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID)))
		want, err := rows.Read(summaries().OrderBy(expr.Value(models.Users.Nickname).Asc(), expr.Value(models.Users.Email).Desc())).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, ordered, want)
		first, err := ordered.Paginate(t.Context(), tx, orm.CursorRequest[reports.UserSummary]{Size: 1})
		if err != nil {
			return err
		}
		for _, changed := range []rows.CursorQuery[reports.UserSummary]{
			ordered.Where(rows.At(cursor.Scope(), reports.UserSummaries.Email).Ne(users[0].Email)),
			cursor.OrderBy(rows.At(cursor.Scope(), reports.UserSummaries.Nickname).Desc()).UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID))),
			cursor.OrderBy(rows.At(cursor.Scope(), reports.UserSummaries.Nickname).Asc(), rows.At(cursor.Scope(), reports.UserSummaries.Email).Desc()).UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID)), expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.Email))),
		} {
			page, err := changed.Paginate(t.Context(), queryfixture.NoQueries(t), orm.CursorRequest[reports.UserSummary]{Size: 1, After: first.Next})
			if !errors.Is(err, fault.Invalid) || page.Items != nil {
				t.Fatal("cross-query cursor accepted", err)
			}
		}
		// Input ordering/windows identify a bounded dataset independently of
		// the order used to traverse it. No inner limit/offset is discarded.
		input := rows.Offset(rows.Limit(summaries().OrderBy(expr.Value(models.Users.Email).Asc()), 2), 1)
		bounded := rows.CursorFor(rows.RecordOf(input))
		boundedWant, err := rows.Read(input).All(t.Context(), tx)
		if err != nil {
			return err
		}
		checkTraversal(t, tx, bounded.UniqueBy(expr.Group(rows.At(bounded.Scope(), reports.UserSummaries.Email))), boundedWant)
		return nil
	})
}

type reportAlias struct{}
type buyerAlias struct{}
type orderAlias struct{}

func TestPostgresRecordCursorMetadataSurvivesComposition(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		input := summaries()
		cte := rows.CTE("foundry_cursor", rows.RecordOf(input))
		a := rows.AsRecord[reportAlias](cte, "summary")
		combined := rows.Union(rows.RecordOf(input), rows.RecordOf(input))
		want, err := rows.Read(input.OrderBy(expr.Value(models.Users.Email).Asc())).All(t.Context(), tx)
		if err != nil {
			return err
		}
		for _, source := range []rows.RecordSource[reports.UserSummary]{cte, rows.SelectRecord(a, rows.Scope(a)), combined} {
			cursor := rows.CursorFor(source)
			checkTraversal(t, tx, cursor.OrderBy(rows.At(cursor.Scope(), reports.UserSummaries.Email).Asc()).UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID))), want)
		}
		users := models.Users.Query()
		for _, source := range []rows.RecordSource[models.User]{rows.ModelRecord(users), rows.SelectRecord(rows.From(users), rows.ModelScope(users)), rows.CTE("cursor_users", rows.ModelRecord(users)), rows.Union(rows.ModelRecord(users), rows.ModelRecord(users))} {
			cursor := rows.CursorFor(source)
			want, err := users.OrderBy(models.Users.Email.Asc()).All(t.Context(), tx)
			if err != nil {
				return err
			}
			checkTraversal(t, tx, cursor.OrderBy(expr.At(cursor.Scope(), models.Users.Email).Asc()).UniqueBy(expr.Group(expr.At(cursor.Scope(), models.Users.Key()))), want)
		}
		return nil
	})
}

func TestPostgresRecursiveResultCursor(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u := models.Users
		tree := rows.RecursiveCTE("cursor_tree", rows.ModelRecord(models.Users.Query().Where(u.Key().Eq(users[0].ID))), func(self rows.RecursiveSelf[models.User]) rows.RecordSource[models.User] {
			parent := rows.AsRecord[buyerAlias](self, "parent")
			child := rows.As[orderAlias](models.Users.Query(), "child")
			joined := rows.InnerJoin(child, parent, rows.On(expr.At(rows.Scope(child), models.Users.IntroducerID), expr.At(rows.Scope(parent), models.Users.Key())))
			return rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(child)))
		})
		cursor := rows.CursorFor(tree)
		checkTraversal(t, tx, cursor.OrderBy(expr.At(cursor.Scope(), models.Users.Email).Asc()).UniqueBy(expr.Group(expr.At(cursor.Scope(), models.Users.Key()))), users[:2])
		return nil
	})
}

func TestCursorValidationBeforeExecution(t *testing.T) {
	cursor := rows.CursorFor(rows.RecordOf(summaries()))
	q := cursor.UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.Paginate(ctx, queryfixture.NoQueries(t), orm.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, bad := range []rows.CursorQuery[reports.UserSummary]{cursor, cursor.UniqueBy(expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID)), expr.Group(rows.At(cursor.Scope(), reports.UserSummaries.ID))), q.OrderBy(rows.At(cursor.Scope(), reports.UserSummaries.ID).Asc(), rows.At(cursor.Scope(), reports.UserSummaries.ID).Desc()), {}} {
		if p, err := bad.Paginate(t.Context(), queryfixture.NoQueries(t), orm.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, fault.Invalid) || p.Items != nil {
			t.Fatal("invalid cursor reached execution", err)
		}
	}
	for _, request := range []orm.CursorRequest[reports.UserSummary]{{}, {Size: orm.MaxPageSize + 1}, {Size: 1, After: value.Set(orm.Cursor[reports.UserSummary]{}), Before: value.Set(orm.Cursor[reports.UserSummary]{})}} {
		if _, err := q.Paginate(t.Context(), queryfixture.NoQueries(t), request); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestPostgresCursorFailureDiscardsPage(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		c := rows.CursorFor(rows.RecordOf(summaries()))
		q := c.UniqueBy(expr.Group(rows.At(c.Scope(), reports.UserSummaries.Email)))
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE id = $1`, users[1].ID.String()); err != nil {
			return err
		}
		if page, err := q.Paginate(t.Context(), tx, orm.CursorRequest[reports.UserSummary]{Size: 1}); err == nil || !reflect.DeepEqual(page, orm.CursorPage[reports.UserSummary]{}) {
			t.Fatal("invalid lookahead leaked page", err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'active', email_address = $1 WHERE id = $2`, strings.Repeat("x", orm.MaxCursorBytes+1), users[1].ID.String()); err != nil {
			return err
		}
		if page, err := c.OrderBy(rows.At(c.Scope(), reports.UserSummaries.Email).Desc()).UniqueBy(expr.Group(rows.At(c.Scope(), reports.UserSummaries.ID))).Paginate(t.Context(), tx, orm.CursorRequest[reports.UserSummary]{Size: 1}); !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(page, orm.CursorPage[reports.UserSummary]{}) {
			t.Fatal("oversized token leaked page", err)
		}
		if _, err := models.Users.Query().Count(t.Context(), tx); err != nil {
			return err
		}
		return nil
	})
}
