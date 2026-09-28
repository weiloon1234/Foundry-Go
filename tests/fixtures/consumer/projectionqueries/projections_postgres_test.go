package projectionqueries_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func userSummarySelection() reports.UserSummarySelection[models.User] {
	f := models.UserFields()
	return reports.UserSummarySelection[models.User]{ID: f.ID.Value(), Email: f.Email.Value(), Nickname: f.Nickname.Value(), Status: f.Status.Value()}
}

func TestPostgresDeclaredProjections(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	first, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			// Deliberately omit unselected model columns: this fixture proves
			// that a projection never falls back to whole-model hydration.
			`CREATE TABLE users (id uuid PRIMARY KEY,email_address text NOT NULL,nickname text,status text NOT NULL)`,
			`CREATE TABLE orders (id uuid PRIMARY KEY,buyer_id uuid NOT NULL,total_cents bigint NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO users VALUES ($1,'a@example.test',NULL,'active'),($2,'b@example.test','B','disabled')`, first.String(), second.String()); err != nil {
			return err
		}
		for _, order := range []struct {
			buyer model.ID[models.User]
			total int64
		}{{first, 2}, {first, 3}, {second, 9}} {
			if _, err := models.QueryOrders().Create(t.Context(), tx, models.OrderDraft{}.SetBuyerID(order.buyer).SetTotalCents(order.total)); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		base := models.QueryUsers().OrderBy(models.UserFields().Email.Asc())
		report := reports.SelectUserSummary(base, userSummarySelection())
		statement, err := report.Compile()
		if err != nil {
			return err
		}
		if strings.Contains(statement.SQL(), "birthday") || !strings.Contains(statement.SQL(), `"users"."email_address" AS "contact_email"`) {
			t.Error("projection selected undeclared columns or lost output alias")
		}
		rows, err := report.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			return errors.New("projection row count mismatch")
		}
		if rows[0].ID != first || rows[0].Email != "a@example.test" || !rows[0].Nickname.IsNull() || rows[1].Status != models.StatusDisabled {
			t.Error("projection lost model-owned IDs, nullable values or imported enum codec")
		}
		widened := userSummarySelection()
		widened.Nickname = query.Nullable(models.UserFields().Email.Value())
		promoted, err := reports.SelectUserSummary(base, widened).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if nickname, ok := promoted.Nickname.Get(); !ok || nickname != promoted.Email {
			t.Error("explicit nullable promotion changed the SQL value")
		}
		if n, err := report.Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("projection count incorrect", err)
		}
		if n, err := report.Limit(1).Offset(1).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("projection count ignored selected window", err)
		}
		if exists, err := report.Limit(0).Exists(t.Context(), counter); err != nil || exists {
			t.Error("zero projection window exists", err)
		}
		one, err := report.Offset(1).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if one.ID != second {
			t.Error("projection First ignored source order/offset")
		}
		empty, err := report.Where(models.UserFields().Email.Eq("missing")).First(t.Context(), counter)
		if err != nil {
			return err
		}
		if empty.IsSet() {
			t.Error("missing projection returned a record")
		}
		if _, err := report.Limit(0).RequireFirst(t.Context(), counter); !errors.Is(err, database.NotFound) {
			t.Error("required projection did not report NotFound")
		}
		stop := errors.New("stop stream")
		seen := 0
		if err := report.Each(t.Context(), counter, func(reports.UserSummary) error { seen++; return stop }); !errors.Is(err, stop) || seen != 1 {
			t.Error("projection did not stop and close its stream")
		}
		if _, err := report.Count(t.Context(), counter); err != nil {
			return err
		}
		fields := models.OrderFields()
		selection := reports.BuyerTotalsSelection[models.Order]{BuyerID: fields.BuyerID.Value(), Total: fields.TotalCents.Sum().Value(), Orders: query.Count[models.Order]().Value(), Average: fields.TotalCents.Avg().Value()}
		totals := reports.SelectBuyerTotals(models.QueryOrders(), selection).GroupBy(fields.BuyerID.Group()).OrderBy(fields.BuyerID.Asc())
		grouped, err := totals.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(grouped) != 2 {
			return errors.New("projection grouping count mismatch")
		}
		for _, row := range grouped {
			total, present := row.Total.Get()
			average, averaged := row.Average.Get()
			if !present || !averaged {
				t.Error("grouped numeric values are unexpectedly NULL")
			}
			if row.BuyerID == first && (row.Orders != 2 || total.String() != "5" || average.String() != "2.5") {
				t.Error("grouped report changed exact numeric/count values")
			}
			if row.BuyerID == second && (row.Orders != 1 || total.String() != "9") {
				t.Error("grouped report attached wrong buyer")
			}
		}
		if n, err := totals.Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("grouped projection count counted input rows", err)
		}
		if n, err := totals.Where(fields.BuyerID.Eq(first)).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("grouped projection lost typed input filters", err)
		}
		ranked := reports.SelectBuyerTotals(models.QueryOrders(), selection).
			GroupBy(fields.BuyerID.Group()).OrderBy(fields.TotalCents.Sum().Desc(), fields.BuyerID.Asc())
		ranking, err := ranked.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(ranking) != 2 || ranking[0].BuyerID != second || ranking[1].BuyerID != first {
			t.Error("aggregate ordering did not rank totals")
		}
		qualified := ranked.Having(query.HavingAnd(query.Count[models.Order]().Gte(2), fields.TotalCents.Sum().Gt(decimal.FromInt64(4))))
		qualifiedRows, err := qualified.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(qualifiedRows) != 1 || qualifiedRows[0].BuyerID != first {
			t.Error("HAVING did not filter grouped totals")
		}
		if n, err := qualified.Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("Count ignored HAVING", err)
		}
		if exists, err := qualified.Offset(1).Exists(t.Context(), counter); err != nil || exists {
			t.Error("Exists ignored filtered window", err)
		}
		mixed := ranked.Where(fields.TotalCents.Gt(2)).Having(query.HavingOr(
			query.Grouped(fields.BuyerID.Eq(first)), fields.TotalCents.Sum().Lt(decimal.FromInt64(9)),
		))
		mixedRows, err := mixed.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(mixedRows) != 1 || mixedRows[0].BuyerID != first {
			t.Error("WHERE and grouped OR predicates ran at the wrong phase")
		}
		if len(mixedRows) == 1 {
			total, _ := mixedRows[0].Total.Get()
			if total.String() != "3" {
				t.Error("WHERE did not filter aggregate inputs")
			}
		}
		if n, err := ranked.Having(query.Count[models.Order]().In()).Count(t.Context(), counter); err != nil || n != 0 {
			t.Error("empty aggregate membership matched groups", err)
		}
		scalar := reports.SelectOrderSummary(models.QueryOrders(), reports.OrderSummarySelection[models.Order]{Total: fields.TotalCents.Sum().Value(), Count: query.Count[models.Order]().Value()})
		summary, err := scalar.RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		total, present := summary.Total.Get()
		if !present || total.String() != "14" || summary.Count != 3 {
			t.Error("scalar aggregate projection changed values")
		}
		summary, err = scalar.Where(fields.TotalCents.Lt(0)).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if !summary.Total.IsNull() || summary.Count != 0 {
			t.Error("empty scalar aggregate projection lost its single NULL/zero result")
		}
		if n, err := scalar.Where(fields.TotalCents.Lt(0)).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("scalar aggregate result count confused with input count", err)
		}
		emptyScalar := scalar.Where(fields.TotalCents.Lt(0))
		for _, check := range []struct {
			condition query.HavingPredicate[models.Order]
			count     int64
		}{
			{fields.TotalCents.Sum().IsNull(), 1}, {fields.TotalCents.Sum().IsNotNull(), 0},
			{fields.TotalCents.Sum().Ne(decimal.FromInt64(0)), 0},
			{fields.TotalCents.Sum().Gt(decimal.FromInt64(0)).Not(), 0},
			{query.Count[models.Order]().Eq(0), 1}, {query.Exists[models.Order]().Eq(false), 1},
		} {
			if n, err := emptyScalar.Having(check.condition).Count(t.Context(), counter); err != nil || n != check.count {
				t.Error("scalar HAVING changed SQL NULL or empty-group semantics", err)
			}
		}
		counter.Queries.Store(0)
		if rows, err := totals.Having(query.Grouped(fields.TotalCents.Gt(0))).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("ungrouped HAVING column executed SQL")
		}
		if rows, err := reports.SelectUserSummary(base, reports.UserSummarySelection[models.User]{}).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("missing projection mapping executed SQL")
		}
		if rows, err := reports.SelectBuyerTotals(models.QueryOrders(), selection).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("ungrouped selected fields executed SQL")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if rows, err := report.All(ctx, counter); !errors.Is(err, context.Canceled) || rows != nil || counter.Queries.Load() != 0 {
			t.Error("canceled projection executed SQL")
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, second.String()); err != nil {
			return err
		}
		if rows, err := report.All(t.Context(), counter); err == nil || rows != nil {
			t.Error("bad projected enum published partial result")
		}
		if _, err := report.Count(t.Context(), counter); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
