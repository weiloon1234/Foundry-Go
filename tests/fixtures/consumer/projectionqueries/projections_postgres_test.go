package projectionqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import "time"

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func userSummarySelection() []rows.Mapping[models.User, reports.UserSummary] {
	f := models.Users
	return []rows.Mapping[models.User, reports.UserSummary]{
		rows.Map(reports.UserSummaries.ID, expr.Value(orm.KeyExpr(f.ID))), rows.Map(reports.UserSummaries.Email, expr.Value(orm.TextExpr(f.Email))), rows.Map(reports.UserSummaries.Nickname, expr.Value(orm.NullableTextExpr(f.Nickname))), rows.Map(reports.UserSummaries.Status, expr.Value(orm.EnumExpr(f.Status))),
	}
}

func TestPostgresDeclaredProjections(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	first, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewIDAt[models.User](time.Now())
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
			if _, err := orm.Insert(t.Context(), tx, models.Orders.Table, orm.ColumnSet(models.Orders.BuyerID, order.buyer), orm.NumberSet(models.Orders.TotalCents, order.total)); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		base := models.Users.Query().OrderBy(orm.TextAsc(models.Users.Email))
		report := rows.Select(rows.From(base), reports.UserSummaries, userSummarySelection()...)
		statement, err := rows.CompileRead(rows.Read(report))
		if err != nil {
			return err
		}
		if strings.Contains(statement.SQL(), "birthday") || !strings.Contains(statement.SQL(), `"users"."email_address" AS "contact_email"`) {
			t.Error("projection selected undeclared columns or lost output alias")
		}
		items, err := rows.Read(report).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			return errors.New("projection row count mismatch")
		}
		if items[0].ID != first || items[0].Email != "a@example.test" || !items[0].Nickname.IsNull() || items[1].Status != models.StatusDisabled {
			t.Error("projection lost model-owned IDs, nullable values or imported enum codec")
		}
		widened := userSummarySelection()
		widened[2] = rows.MapSelected(reports.UserSummaries.Nickname, expr.Row(expr.NullableRow(expr.Value(orm.TextExpr(models.Users.Email)))).Value())
		promoted, err := rows.Read(rows.Select(rows.From(base), reports.UserSummaries, widened...)).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if nickname, ok := promoted.Nickname.Get(); !ok || nickname != promoted.Email {
			t.Error("explicit nullable promotion changed the SQL value")
		}
		if n, err := rows.Read(report).Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("projection count incorrect", err)
		}
		if n, err := rows.Read(rows.Offset(rows.Limit(report, 1), 1)).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("projection count ignored selected window", err)
		}
		if exists, err := rows.Read(rows.Limit(report, 0)).Exists(t.Context(), counter); err != nil || exists {
			t.Error("zero projection window exists", err)
		}
		one, err := rows.Read(rows.Offset(report, 1)).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if one.ID != second {
			t.Error("projection First ignored source order/offset")
		}
		empty, err := rows.Read(report.Where(expr.ModelPredicate(orm.TextEq(models.Users.Email, "missing")))).First(t.Context(), counter)
		if err != nil {
			return err
		}
		if empty.IsSet() {
			t.Error("missing projection returned a record")
		}
		if _, err := rows.Read(rows.Limit(report, 0)).RequireFirst(t.Context(), counter); !errors.Is(err, database.NotFound) {
			t.Error("required projection did not report NotFound")
		}
		stop := errors.New("stop stream")
		seen := 0
		if err := rows.EachRead(t.Context(), counter, rows.Read(report), func(reports.UserSummary) error { seen++; return stop }); !errors.Is(err, stop) || seen != 1 {
			t.Error("projection did not stop and close its stream")
		}
		if _, err := rows.Read(report).Count(t.Context(), counter); err != nil {
			return err
		}
		fields := models.Orders
		selection := []rows.Mapping[models.Order, reports.BuyerTotals]{rows.Map(reports.BuyerTotalRows.BuyerID, expr.Value(orm.ColumnExpr(fields.BuyerID))), rows.MapSelected(reports.BuyerTotalRows.Total, expr.Sum(orm.NumberExpr(fields.TotalCents)).Value()), rows.MapSelected(reports.BuyerTotalRows.Orders, expr.Count[models.Order]().Value()), rows.MapSelected(reports.BuyerTotalRows.Average, expr.Avg(orm.NumberExpr(fields.TotalCents)).Value())}
		totals := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(models.Orders.Query()), reports.BuyerTotalRows, selection...), expr.Group(expr.Value(orm.ColumnExpr(fields.BuyerID)))), expr.Value(orm.ColumnExpr(fields.BuyerID)).Asc())
		grouped, err := rows.Read(totals).All(t.Context(), counter)
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
		if n, err := rows.Read(totals).Count(t.Context(), counter); err != nil || n != 2 {
			t.Error("grouped projection count counted input items", err)
		}
		if n, err := rows.Read(totals.Where(expr.ModelPredicate(orm.ColumnEq(fields.BuyerID, first)))).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("grouped projection lost typed input filters", err)
		}
		ranked := rows.OrderBy(rows.GroupBy(rows.Select(rows.From(models.Orders.Query()), reports.BuyerTotalRows, selection...), expr.Group(expr.Value(orm.ColumnExpr(fields.BuyerID)))), expr.Sum(orm.NumberExpr(fields.TotalCents)).Desc(), expr.Value(orm.ColumnExpr(fields.BuyerID)).Asc())
		ranking, err := rows.Read(ranked).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(ranking) != 2 || ranking[0].BuyerID != second || ranking[1].BuyerID != first {
			t.Error("aggregate ordering did not rank totals")
		}
		qualified := rows.Having(ranked, expr.HavingAnd(expr.Count[models.Order]().Gte(2), expr.Sum(orm.NumberExpr(fields.TotalCents)).Gt(decimal.FromInt64(4))))
		qualifiedRows, err := rows.Read(qualified).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(qualifiedRows) != 1 || qualifiedRows[0].BuyerID != first {
			t.Error("HAVING did not filter grouped totals")
		}
		if n, err := rows.Read(qualified).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("Count ignored HAVING", err)
		}
		if exists, err := rows.Read(rows.Offset(qualified, 1)).Exists(t.Context(), counter); err != nil || exists {
			t.Error("Exists ignored filtered window", err)
		}
		mixed := rows.Having(ranked.Where(expr.ModelPredicate(orm.NumberGt(fields.TotalCents, 2))), expr.HavingOr(
			expr.Grouped(expr.ModelPredicate(orm.ColumnEq(fields.BuyerID, first))), expr.Sum(orm.NumberExpr(fields.TotalCents)).Lt(decimal.FromInt64(9)),
		))
		mixedRows, err := rows.Read(mixed).All(t.Context(), counter)
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
		if n, err := rows.Read(rows.Having(ranked, expr.Count[models.Order]().In())).Count(t.Context(), counter); err != nil || n != 0 {
			t.Error("empty aggregate membership matched groups", err)
		}
		scalar := rows.Select(rows.From(models.Orders.Query()), reports.OrderSummaries, rows.MapSelected(reports.OrderSummaries.Total, expr.Sum(orm.NumberExpr(fields.TotalCents)).Value()), rows.MapSelected(reports.OrderSummaries.Count, expr.Count[models.Order]().Value()))
		summary, err := rows.Read(scalar).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		total, present := summary.Total.Get()
		if !present || total.String() != "14" || summary.Count != 3 {
			t.Error("scalar aggregate projection changed values")
		}
		summary, err = rows.Read(scalar.Where(expr.ModelPredicate(orm.NumberLt(fields.TotalCents, 0)))).RequireFirst(t.Context(), counter)
		if err != nil {
			return err
		}
		if !summary.Total.IsNull() || summary.Count != 0 {
			t.Error("empty scalar aggregate projection lost its single NULL/zero result")
		}
		if n, err := rows.Read(scalar.Where(expr.ModelPredicate(orm.NumberLt(fields.TotalCents, 0)))).Count(t.Context(), counter); err != nil || n != 1 {
			t.Error("scalar aggregate result count confused with input count", err)
		}
		emptyScalar := scalar.Where(expr.ModelPredicate(orm.NumberLt(fields.TotalCents, 0)))
		for _, check := range []struct {
			condition expr.HavingPredicate[models.Order]
			count     int64
		}{
			{expr.Sum(orm.NumberExpr(fields.TotalCents)).IsNull(), 1}, {expr.Sum(orm.NumberExpr(fields.TotalCents)).IsNotNull(), 0},
			{expr.Sum(orm.NumberExpr(fields.TotalCents)).Ne(decimal.FromInt64(0)), 0},
			{expr.Sum(orm.NumberExpr(fields.TotalCents)).Gt(decimal.FromInt64(0)).Not(), 0},
			{expr.Count[models.Order]().Eq(0), 1}, {expr.Exists[models.Order]().Eq(false), 1},
		} {
			if n, err := rows.Read(rows.Having(emptyScalar, check.condition)).Count(t.Context(), counter); err != nil || n != check.count {
				t.Error("scalar HAVING changed SQL NULL or empty-group semantics", err)
			}
		}
		counter.Queries.Store(0)
		if items, err := rows.Read(rows.Having(totals, expr.Grouped(expr.ModelPredicate(orm.NumberGt(fields.TotalCents, 0))))).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || items != nil || counter.Queries.Load() != 0 {
			t.Error("ungrouped HAVING column executed SQL")
		}
		if items, err := rows.Read(rows.Select(rows.From(base), reports.UserSummaries)).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || items != nil || counter.Queries.Load() != 0 {
			t.Error("missing projection mapping executed SQL")
		}
		if items, err := rows.Read(rows.Select(rows.From(models.Orders.Query()), reports.BuyerTotalRows, selection...)).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || items != nil || counter.Queries.Load() != 0 {
			t.Error("ungrouped selected fields executed SQL")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if items, err := rows.Read(report).All(ctx, counter); !errors.Is(err, context.Canceled) || items != nil || counter.Queries.Load() != 0 {
			t.Error("canceled projection executed SQL")
		}
		if _, err := tx.Exec(t.Context(), `UPDATE users SET status='invalid' WHERE id=$1`, second.String()); err != nil {
			return err
		}
		if items, err := rows.Read(report).All(t.Context(), counter); err == nil || items != nil {
			t.Error("bad projected enum published partial result")
		}
		if _, err := rows.Read(report).Count(t.Context(), counter); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
