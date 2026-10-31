package temporalqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type annualSummary struct{}
type previousSample struct{}

func TestPostgresTemporalComposition(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		q := temporalqueries.Samples.Query()
		f := temporalqueries.Samples
		year := expr.Year(expr.Value(orm.TimeExpr(f.Date)))
		projected := rows.GroupBy(rows.Select(rows.From(q), temporalqueries.CalendarSummaries, rows.MapSelected(temporalqueries.CalendarSummaries.Year, expr.AsSelected(year)), rows.MapSelected(temporalqueries.CalendarSummaries.Rows, expr.AsSelected(expr.Count[temporalqueries.Sample]())), rows.MapSelected(temporalqueries.CalendarSummaries.Latest, expr.AsSelected(expr.Max(orm.TimeExpr(f.Date))))), expr.Group(year))
		derived := rows.AsRecord[annualSummary](rows.CTE("annual", rows.RecordOf(projected)), "calendar")
		derivedScope := rows.Scope(derived)
		if items, err := rows.ReadScalar(rows.SelectScalar(derived, expr.AsSelected(expr.YearNullable(rows.At(derivedScope, temporalqueries.CalendarSummaries.Latest))))).Where(expr.Gt(expr.OrderRow(rows.At(derivedScope, temporalqueries.CalendarSummaries.Rows)), 1)).All(t.Context(), tx); err != nil || len(items) != 1 || items[0] != value.Of(int64(2024)) {
			t.Fatal(items, err)
		}
		previous := rows.As[previousSample](q, "previous")
		link := rows.Correlate(rows.From(q), previous)
		parent := rows.OuterScope(link, rows.ModelScope(q))
		child := rows.InnerScope(link, rows.Scope(previous))
		selected := rows.SelectCorrelatedScalar(link, expr.AsSelected(expr.DateDifference(expr.At(parent, orm.TimeExpr(temporalqueries.Samples.Date)), expr.At(child, orm.TimeExpr(temporalqueries.Samples.Date))))).Where(expr.Less(expr.At(child, orm.NumberKeyExpr(temporalqueries.Samples.ID)), expr.At(parent, orm.NumberKeyExpr(temporalqueries.Samples.ID)))).OrderBy(expr.Desc(expr.At(child, orm.NumberKeyExpr(temporalqueries.Samples.ID)))).Limit(1)
		var want []value.Nullable[int64]
		for i, s := range samples {
			if i == 0 {
				want = append(want, value.Null[int64]())
				continue
			}
			a := time.Date(s.Date.Year(), s.Date.Month(), s.Date.Day(), 0, 0, 0, 0, time.UTC)
			d := samples[i-1].Date
			b := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
			want = append(want, value.Of((a.Unix()-b.Unix())/86400))
		}
		temporalValues(t, tx, expr.AsSelected(rows.CorrelatedScalarRowQuery(selected)), want)
		window := expr.WindowFor(rows.ModelScope(q)).OrderBy(expr.Asc(expr.Value(orm.NumberKeyExpr(f.ID))))
		roundtrip := expr.UnixMillisecondsValue(expr.FromUnixMillisValue(expr.RowNumber(window)))
		temporalValues(t, tx, roundtrip, []int64{1, 2, 3, 4})
		var nullable []value.Nullable[temporal.LocalDateTime]
		for _, s := range samples {
			nullable = append(nullable, s.MaybeLocal)
		}
		temporalValues(t, tx, expr.AsSelected(expr.CombineDateTimeNullable(expr.Value(orm.NullableTimeExpr(f.MaybeDate)), expr.Value(orm.NullableTimeExpr(f.MaybeClock)))), nullable)
		temporalValues(t, tx, expr.AsSelected(expr.AddLocalIntervalNullable(expr.Value(orm.NullableTimeExpr(f.MaybeLocal)), temporal.Interval{})), nullable)
		years := expr.Coalesce(expr.YearNullable(expr.Value(orm.NullableTimeExpr(f.MaybeDate))), expr.ParamOf(expr.Year(expr.Value(orm.TimeExpr(f.Date))), 0))
		temporalValues(t, tx, expr.AsSelected(years), []int64{2024, 0, 1969, 2019})
		before := temporalOne(t, tx, expr.AsSelected(expr.TransactionTime(rows.ModelScope(q))))
		if count, err := orm.Count(t.Context(), tx, q); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		after := temporalOne(t, tx, expr.AsSelected(expr.TransactionTime(rows.ModelScope(q))))
		if before != after || before.UTC().Location() != time.UTC {
			t.Fatal("transaction time changed", before, after)
		}
		return nil
	})
}

func TestPostgresTemporalFailureBoundaries(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, _ []temporalqueries.Sample) error {
		q := temporalqueries.Samples.Query()
		f := temporalqueries.Samples
		err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.FromUnixMillis(expr.Param(orm.NumberExpr(f.Millis), math.MaxInt64))))).All(t.Context(), inner)
			return err
		})
		var databaseError *database.Error
		if !errors.As(err, &databaseError) || databaseError.SQLState() != "22015" {
			t.Fatal("unrepresentable epoch", err)
		}
		for _, millis := range []int64{-62135596800001, 253402300800000} {
			if items, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.FromUnixMillis(expr.Param(orm.NumberExpr(f.Millis), millis))))).All(t.Context(), tx); err == nil || items != nil {
				t.Fatal("out-of-contract year published results", items, err)
			}
		}
		return nil
	})
}

func TestTemporalFailuresBeforeExecution(t *testing.T) {
	q := temporalqueries.Samples.Query()
	f := temporalqueries.Samples
	if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Limit(0)), expr.AsSelected(expr.TruncateDate(expr.Value(orm.TimeExpr(f.Date)), 0)))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Limit(0)), expr.AsSelected(expr.TruncateInstant(expr.Value(orm.TimeExpr(f.At)), expr.TimestampDay, expr.TimeZone{})))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Limit(0)), expr.AsSelected(expr.ResolveLocal(expr.Value(orm.TimeExpr(f.Local)), expr.UTCZone(), 0)))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q.Limit(0)), expr.AsSelected(expr.AddClockElapsed(expr.Value(orm.TimeExpr(f.Clock)), time.Nanosecond)))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.Year(expr.Value(orm.TimeExpr(f.Date)))))).All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
