package intervalqueries_test

import (
	"math"
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestPostgresIntervalOutputStyles(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, _ []intervalqueries.Sample) error {
		f := intervalqueries.SampleFields()
		cases := []temporal.Interval{{}, temporal.Months(1), temporal.Days(3), temporal.Days(-3),
			interval(t, 14, 3, 25*time.Hour+time.Microsecond), interval(t, -14, -3, -25*time.Hour-time.Microsecond),
			interval(t, -14, 3, -25*time.Hour-time.Microsecond), interval(t, 14, -3, 25*time.Hour+time.Microsecond),
			interval(t, math.MinInt32, math.MinInt32, (time.Duration(math.MinInt64)/time.Microsecond)*time.Microsecond),
			interval(t, math.MaxInt32, math.MaxInt32, (time.Duration(math.MaxInt64)/time.Microsecond)*time.Microsecond),
		}
		for _, style := range []string{"postgres", "postgres_verbose", "sql_standard", "iso_8601"} {
			rows, err := tx.Query(t.Context(), `SELECT set_config('IntervalStyle',$1,true)`, style)
			if err != nil {
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			for _, want := range cases {
				if got := intervalOne(t, tx, f.Period.Param(want).Value()); got != want {
					t.Fatal(style, got, want)
				}
			}
			// Test the actual driver text boundary and leave destinations unchanged on failure.
			for _, text := range []string{"9223372036854776 microseconds", "infinity", "-infinity"} {
				rows, err := tx.Query(t.Context(), `SELECT CAST($1 AS interval)`, text)
				if err != nil {
					return err
				}
				got := temporal.Days(7)
				if !rows.Next() {
					t.Fatal(rows.Err())
				}
				err = rows.Scan(codec.Interval().Scan(&got))
				closeErr := rows.Close()
				if err == nil || closeErr == nil || got != temporal.Days(7) {
					t.Fatal(style, got, err, closeErr)
				}
			}
		}
		return nil
	})
}

func TestPostgresIntervalWritesAndDecodeFailure(t *testing.T) {
	runIntervals(t, func(tx *database.Tx, _ []intervalqueries.Sample) error {
		q := intervalqueries.QueryIntervalSamples()
		f := intervalqueries.SampleFields()
		got, err := q.Update(t.Context(), tx, 2, intervalqueries.SampleDraft{}.SetPeriod(temporal.Days(4)).SetMaybePeriod(temporal.Interval{}))
		if err != nil || got.Period != temporal.Days(4) || got.MaybePeriod.IsNull() {
			t.Fatal(got, err)
		}
		got, err = q.Update(t.Context(), tx, 2, intervalqueries.SampleDraft{}.ClearMaybePeriod())
		if err != nil || got.Period != temporal.Days(4) || !got.MaybePeriod.IsNull() {
			t.Fatal(got, err)
		}
		if _, err := tx.Exec(t.Context(), `UPDATE interval_samples SET period = INTERVAL '9223372036854776 microseconds' WHERE id=4`); err != nil {
			return err
		}
		if rows, err := q.OrderBy(f.ID.Asc()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("bad later row published partial models", rows, err)
		}
		if rows, err := query.SelectValue(q.OrderBy(f.ID.Asc()), f.Period.Value()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("bad later row published partial values", rows, err)
		}
		return nil
	})
}
