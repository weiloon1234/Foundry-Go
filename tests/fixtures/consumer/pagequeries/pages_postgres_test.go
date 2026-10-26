package pagequeries_test

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

type pageExecutor struct {
	database.Executor
	statements []string
	failAt     int
	err        error
}

func (e *pageExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	e.statements = append(e.statements, sql)
	if len(e.statements) == e.failAt {
		return nil, e.err
	}
	return e.Executor.Query(ctx, sql, args...)
}

func summaries() rows.Query[models.User, reports.UserSummary] {
	u, r := models.Users, reports.UserSummaries
	return rows.Select(rows.From(u.Query()), r.Row, rows.MapSelected(r.ID, expr.Row(expr.Value(u.Key())).Value()), rows.MapSelected(r.Email, expr.Row(expr.Value(u.Email)).Value()), rows.MapSelected(r.Nickname, expr.Row(expr.Value(u.Nickname)).Value()), rows.MapSelected(r.Status, expr.Row(expr.Value(u.Status)).Value())).OrderBy(expr.Value(u.Email).Asc(), expr.Value(u.Key()).Asc())
}

func TestPostgresSimpleModelsAndEagerLookahead(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base := models.Users.Query().OrderBy(models.Users.Age.Asc())
		for _, number := range []int{1, 2, 3, 4, 99} {
			e := &pageExecutor{Executor: tx}
			page, err := base.SimplePaginate(t.Context(), e, orm.PageRequest{Number: number, Size: 1})
			if err != nil {
				return err
			}
			if len(e.statements) != 1 || strings.Contains(e.statements[0], "COUNT(") {
				t.Fatal("simple pagination counted")
			}
			if page.Number != number || page.Size != 1 || page.HasMore != (number < 3) {
				t.Fatal("wrong simple metadata", page)
			}
			if number <= 3 {
				if len(page.Items) != 1 || page.Items[0].ID != users[number-1].ID {
					t.Fatal("wrong simple row")
				}
			} else if len(page.Items) != 0 {
				t.Fatal("beyond-last page not empty")
			}
		}
		limits := orm.DefaultRelationLimits()
		limits.MaxRows = 2
		eager := orm.WithRelationLimits(base.With(models.UserLinks.Orders), limits)
		e := &pageExecutor{Executor: tx}
		page, err := eager.SimplePaginate(t.Context(), e, orm.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		orders, loaded := page.Items[0].Orders.Get()
		if !loaded || len(orders) != 2 || !page.HasMore || len(e.statements) != 2 {
			t.Fatal("eager page loaded lookahead relations")
		}
		// The same lookahead ownership contract applies to model cursor pages.
		cursor, err := orm.CursorPaginate(t.Context(), tx, eager, orm.CursorRequest[models.User]{Size: 1})
		if err != nil {
			return err
		}
		if len(cursor.Items) != 1 || !cursor.Next.IsSet() {
			t.Fatal("cursor lost lookahead")
		}
		next, _ := cursor.Next.Get()
		forward, err := orm.CursorPaginate(t.Context(), tx, eager, orm.CursorRequest[models.User]{Size: 1, After: value.Set(next)})
		if err != nil {
			return err
		}
		previous, _ := forward.Previous.Get()
		back, err := orm.CursorPaginate(t.Context(), tx, eager, orm.CursorRequest[models.User]{Size: 1, Before: value.Set(previous)})
		if err != nil || len(back.Items) != 1 || back.Items[0].ID != users[0].ID {
			t.Fatal("backward eager cursor changed", err)
		}
		stop := errors.New("related query failed")
		failed := &pageExecutor{Executor: tx, failAt: 2, err: stop}
		if result, err := eager.SimplePaginate(t.Context(), failed, orm.PageRequest{Number: 1, Size: 1}); !errors.Is(err, stop) || !reflect.DeepEqual(result, orm.SimplePage[models.User]{}) {
			t.Fatal("partial eager page escaped", err)
		}
		return nil
	})
}

func TestPostgresProjectionPagesAndFailureDisposal(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := summaries()
		for _, number := range []int{1, 2, 3, 99} {
			e := &pageExecutor{Executor: tx}
			page, err := rows.Read(q).Paginate(t.Context(), e, orm.PageRequest{Number: number, Size: 2})
			if err != nil {
				return err
			}
			if len(e.statements) != 2 || page.Total != 3 || page.Pages != 2 || page.Number != number || page.Size != 2 {
				t.Fatal("wrong projection page metadata", page)
			}
			simple, err := rows.Read(q).SimplePaginate(t.Context(), tx, orm.PageRequest{Number: number, Size: 2})
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(simple.Items, page.Items) || simple.HasMore != (number == 1) {
				t.Fatal("projection simple page differs")
			}
			if number == 1 && (page.Items[0].ID != users[0].ID || page.Items[1].Nickname != value.Of("Bee") || page.Items[0].Status != models.StatusActive) {
				t.Fatal("page lost named columns or codecs")
			}
		}
		for _, failAt := range []int{1, 2} {
			stop := errors.New("page query failed")
			e := &pageExecutor{Executor: tx, failAt: failAt, err: stop}
			p, err := rows.Read(q).Paginate(t.Context(), e, orm.PageRequest{Number: 1, Size: 2})
			if !errors.Is(err, stop) || !reflect.DeepEqual(p, orm.PageResult[reports.UserSummary]{}) || len(e.statements) != failAt {
				t.Fatal("failed page leaked metadata", err)
			}
		}
		// Invalid lookahead rows are decoded before disposal; no partial page escapes.
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE email_address = $1`, users[1].Email); err != nil {
			return err
		}
		if p, err := rows.Read(q).SimplePaginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1}); err == nil || p.Items != nil {
			t.Fatal("invalid projection lookahead escaped")
		}
		if p, err := models.Users.Query().OrderBy(models.Users.Email.Asc()).SimplePaginate(t.Context(), tx, orm.PageRequest{Number: 1, Size: 1}); err == nil || p.Items != nil {
			t.Fatal("invalid model lookahead escaped")
		}
		return nil
	})
}

type reportAlias struct{}
type buyerAlias struct{}
type orderAlias struct{}

func TestPageRejectsInvalidInputsBeforeExecution(t *testing.T) {
	q := summaries()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rows.Read(q).Paginate(ctx, queryfixture.NoQueries(t), orm.PageRequest{Number: 1, Size: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := rows.Read(rows.Limit(q, 1)).SimplePaginate(t.Context(), queryfixture.NoQueries(t), orm.PageRequest{Number: 1, Size: 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := models.Users.Query().SimplePaginate(t.Context(), queryfixture.NoQueries(t), orm.PageRequest{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
