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
	"github.com/weiloon1234/Foundry-Go/database/query"
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

func summaries() query.ProjectionQuery[models.User, reports.UserSummary] {
	u := models.UserFields()
	return reports.SelectUserSummary(models.QueryUsers(), reports.UserSummarySelection[models.User]{ID: u.ID.Value(), Email: u.Email.Value(), Nickname: u.Nickname.Value(), Status: u.Status.Value()}).OrderBy(u.Email.Asc(), u.ID.Asc())
}

func TestPostgresSimpleModelsAndEagerLookahead(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		base := models.QueryUsers().OrderBy(models.UserFields().Age.Asc())
		for _, number := range []int{1, 2, 3, 4, 99} {
			e := &pageExecutor{Executor: tx}
			page, err := base.SimplePaginate(t.Context(), e, query.PageRequest{Number: number, Size: 1})
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
		limits := query.DefaultRelationLimits()
		limits.MaxRows = 2
		eager := base.With(models.UserRelations().Orders).WithRelationLimits(limits)
		e := &pageExecutor{Executor: tx}
		page, err := eager.SimplePaginate(t.Context(), e, query.PageRequest{Number: 1, Size: 1})
		if err != nil {
			return err
		}
		orders, loaded := page.Items[0].Orders.Get()
		if !loaded || len(orders) != 2 || !page.HasMore || len(e.statements) != 2 {
			t.Fatal("eager page loaded lookahead relations")
		}
		// The same lookahead ownership contract applies to model cursor pages.
		cursor, err := eager.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1})
		if err != nil {
			return err
		}
		if len(cursor.Items) != 1 || !cursor.Next.IsSet() {
			t.Fatal("cursor lost lookahead")
		}
		next, _ := cursor.Next.Get()
		forward, err := eager.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1, After: value.Set(next)})
		if err != nil {
			return err
		}
		previous, _ := forward.Previous.Get()
		back, err := eager.CursorPaginate(t.Context(), tx, query.CursorRequest[models.User]{Size: 1, Before: value.Set(previous)})
		if err != nil || len(back.Items) != 1 || back.Items[0].ID != users[0].ID {
			t.Fatal("backward eager cursor changed", err)
		}
		stop := errors.New("related query failed")
		failed := &pageExecutor{Executor: tx, failAt: 2, err: stop}
		if result, err := eager.SimplePaginate(t.Context(), failed, query.PageRequest{Number: 1, Size: 1}); !errors.Is(err, stop) || !reflect.DeepEqual(result, query.SimplePage[models.User]{}) {
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
			page, err := q.Paginate(t.Context(), e, query.PageRequest{Number: number, Size: 2})
			if err != nil {
				return err
			}
			if len(e.statements) != 2 || page.Total != 3 || page.Pages != 2 || page.Number != number || page.Size != 2 {
				t.Fatal("wrong projection page metadata", page)
			}
			simple, err := q.SimplePaginate(t.Context(), tx, query.PageRequest{Number: number, Size: 2})
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
			p, err := q.Paginate(t.Context(), e, query.PageRequest{Number: 1, Size: 2})
			if !errors.Is(err, stop) || !reflect.DeepEqual(p, query.Page[reports.UserSummary]{}) || len(e.statements) != failAt {
				t.Fatal("failed page leaked metadata", err)
			}
		}
		// Invalid lookahead rows are decoded before disposal; no partial page escapes.
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status = 'invalid' WHERE email_address = $1`, users[1].Email); err != nil {
			return err
		}
		if p, err := q.SimplePaginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err == nil || p.Items != nil {
			t.Fatal("invalid projection lookahead escaped")
		}
		if p, err := models.QueryUsers().OrderBy(models.UserFields().Email.Asc()).SimplePaginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1}); err == nil || p.Items != nil {
			t.Fatal("invalid model lookahead escaped")
		}
		return nil
	})
}

type reportAlias struct{}
type buyerAlias struct{}
type orderAlias struct{}

func TestPostgresPagesRetainRelationalSemantics(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		o := models.OrderFields()
		grouped := reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{BuyerID: o.BuyerID.Value(), Total: o.TotalCents.Sum().Value(), Orders: query.Count[models.Order]().Value(), Average: o.TotalCents.Avg().Value()}).GroupBy(o.BuyerID.Group()).Having(query.Count[models.Order]().Gt(0)).OrderBy(o.BuyerID.Asc())
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
		definition := query.CTE("paged_reports", summaries().Limit(2))
		a := query.As[reportAlias](definition, "report")
		f := reports.UserSummaryFieldsAt(a.Scope())
		derived := query.SelectRecord(a, a.Scope()).OrderBy(f.ID.Asc())
		d, err := derived.Paginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 1})
		if err != nil || d.Total != 2 {
			t.Fatal("page discarded nested CTE limit", err)
		}
		combined := summaries().Limit(1).UnionAll(summaries().Limit(2))
		sf := reports.UserSummaryFieldsAt(combined.Scope())
		combined = combined.OrderBy(sf.Email.Asc())
		setPage, err := combined.Paginate(t.Context(), tx, query.PageRequest{Number: 2, Size: 2})
		if err != nil || setPage.Total != 3 || len(setPage.Items) != 1 {
			t.Fatal("set page lost multiplicity/input limits", err)
		}
		if simple, err := combined.SimplePaginate(t.Context(), tx, query.PageRequest{Number: 1, Size: 2}); err != nil || !simple.HasMore {
			t.Fatal("set simple page failed", err)
		}
		values := query.SelectValue(models.QueryUsers(), models.UserFields().Nickname.Value()).Distinct().OrderBy(models.UserFields().Nickname.Asc())
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

func TestPageRejectsInvalidInputsBeforeExecution(t *testing.T) {
	q := summaries()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := q.Paginate(ctx, queryfixture.NoQueries(t), query.PageRequest{Number: 1, Size: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := q.Limit(1).SimplePaginate(t.Context(), queryfixture.NoQueries(t), query.PageRequest{Number: 1, Size: 1}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := models.QueryUsers().SimplePaginate(t.Context(), queryfixture.NoQueries(t), query.PageRequest{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
