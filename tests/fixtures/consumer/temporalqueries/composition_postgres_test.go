package temporalqueries_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type annualSummary struct{}
type previousSample struct{}

func TestPostgresTemporalComposition(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, samples []temporalqueries.Sample) error {
		q := temporalqueries.QueryTemporalSamples()
		f := temporalqueries.SampleFields()
		year := query.Year(f.Date)
		projected := temporalqueries.ProjectCalendarSummary(q).SelectYear(year.Value()).SelectRows(query.Count[temporalqueries.Sample]().Value()).SelectLatest(f.Date.Max().Value()).Query().GroupBy(year.Group())
		derived := query.As[annualSummary](query.CTE("annual", projected), "calendar")
		columns := temporalqueries.CalendarSummaryFieldsAt(derived.Scope())
		if rows, err := query.SelectValue(derived, query.YearNullable(columns.Latest).Value()).Where(columns.Rows.Gt(1)).All(t.Context(), tx); err != nil || len(rows) != 1 || rows[0] != value.Of(int64(2024)) {
			t.Fatal(rows, err)
		}
		previous := query.As[previousSample](q, "previous")
		link := query.Correlate(q, previous)
		parent := temporalqueries.SampleFieldsAt(query.OuterScope(link, q.Scope()))
		child := temporalqueries.SampleFieldsAt(query.InnerScope(link, previous.Scope()))
		selected := query.SelectCorrelatedValue(link, query.DateDifference(parent.Date, child.Date).Value()).Where(child.ID.LtColumn(parent.ID)).OrderBy(child.ID.Desc()).Limit(1)
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
		temporalValues(t, tx, query.CorrelatedScalarQuery(selected), want)
		window := query.WindowFor(q).OrderBy(f.ID.Asc())
		roundtrip := query.UnixMillisecondsValue(query.FromUnixMillisValue(query.RowNumber(window)))
		temporalValues(t, tx, roundtrip, []int64{1, 2, 3, 4})
		var nullable []value.Nullable[temporal.LocalDateTime]
		for _, s := range samples {
			nullable = append(nullable, s.MaybeLocal)
		}
		temporalValues(t, tx, query.CombineDateTimeNullable(f.MaybeDate, f.MaybeClock).Value(), nullable)
		temporalValues(t, tx, query.AddLocalIntervalNullable(f.MaybeLocal, temporal.Interval{}).Value(), nullable)
		years := query.Coalesce(query.YearNullable(f.MaybeDate), query.Year(f.Date).Param(0))
		temporalValues(t, tx, years.Value(), []int64{2024, 0, 1969, 2019})
		before := temporalOne(t, tx, query.TransactionTime(q).Value())
		if count, err := q.Count(t.Context(), tx); err != nil || count != 4 {
			t.Fatal(count, err)
		}
		after := temporalOne(t, tx, query.TransactionTime(q).Value())
		if before != after || before.UTC().Location() != time.UTC {
			t.Fatal("transaction time changed", before, after)
		}
		return nil
	})
}

func TestPostgresTemporalFailureBoundaries(t *testing.T) {
	runTemporal(t, func(tx *database.Tx, _ []temporalqueries.Sample) error {
		q := temporalqueries.QueryTemporalSamples()
		f := temporalqueries.SampleFields()
		err := tx.Savepoint(t.Context(), func(inner *database.Tx) error {
			_, err := query.SelectValue(q, query.FromUnixMillis(f.Millis.Param(math.MaxInt64)).Value()).All(t.Context(), inner)
			return err
		})
		var databaseError *database.Error
		if !errors.As(err, &databaseError) || databaseError.SQLState() != "22015" {
			t.Fatal("unrepresentable epoch", err)
		}
		for _, millis := range []int64{-62135596800001, 253402300800000} {
			if rows, err := query.SelectValue(q, query.FromUnixMillis(f.Millis.Param(millis)).Value()).All(t.Context(), tx); err == nil || rows != nil {
				t.Fatal("out-of-contract year published results", rows, err)
			}
		}
		return nil
	})
}

func TestTemporalFailuresBeforeExecution(t *testing.T) {
	q := temporalqueries.QueryTemporalSamples()
	f := temporalqueries.SampleFields()
	if _, err := query.SelectValue(q.Limit(0), query.TruncateDate(f.Date, 0).Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := query.SelectValue(q.Limit(0), query.TruncateInstant(f.At, query.TimestampDay, query.TimeZone{}).Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := query.SelectValue(q.Limit(0), query.ResolveLocal(f.Local, query.UTCZone(), 0).Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := query.SelectValue(q.Limit(0), query.AddClockElapsed(f.Clock, time.Nanosecond).Value()).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := query.SelectValue(q, query.Year(f.Date).Value()).All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
