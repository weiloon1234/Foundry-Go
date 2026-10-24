package intervalqueries_test

import (
	"testing"
	"time"

	"foundry.test/consumer/intervalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func interval(t *testing.T, months, days int32, elapsed time.Duration) temporal.Interval {
	t.Helper()
	v, err := temporal.NewInterval(months, days, elapsed)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func runIntervals(t *testing.T, check func(*database.Tx, []intervalqueries.Sample) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`SET LOCAL TIME ZONE 'Pacific/Auckland'`,
			`CREATE TABLE interval_samples (id bigint PRIMARY KEY,period interval NOT NULL,maybe_period interval,"start" timestamptz NOT NULL,"end" timestamptz NOT NULL,local_start timestamp NOT NULL,local_end timestamp NOT NULL,clock_start time NOT NULL,clock_end time NOT NULL,"date" date NOT NULL)`,
			`CREATE TABLE interval_plans (id bigint PRIMARY KEY,period interval NOT NULL)`,
			`CREATE TABLE interval_labels (id interval PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE interval_links (id interval PRIMARY KEY,plan_period interval NOT NULL,label_id interval NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		start := time.Date(2024, 3, 9, 17, 0, 0, 0, time.UTC)
		end, err := temporal.NewDateTime(start.Add(25 * time.Hour))
		if err != nil {
			return err
		}
		localStart, err := temporal.ParseLocalDateTime("2024-03-09T12:00:00")
		if err != nil {
			return err
		}
		localEnd, err := temporal.ParseLocalDateTime("2024-03-10T13:00:00")
		if err != nil {
			return err
		}
		clockStart, err := temporal.ParseTime("23:00:00")
		if err != nil {
			return err
		}
		clockEnd, err := temporal.ParseTime("01:00:00")
		if err != nil {
			return err
		}
		date, err := temporal.ParseDate("2024-01-31")
		if err != nil {
			return err
		}
		var samples []intervalqueries.Sample
		for i, period := range []temporal.Interval{temporal.Months(1), temporal.Days(30), interval(t, 0, 0, 720*time.Hour), {}} {
			f := intervalqueries.Samples
			assignments := []orm.Assignment[intervalqueries.Sample]{f.ID.Set(i + 1), f.Period.Set(period), f.Start.Set(start), f.End.Set(end), f.LocalStart.Set(localStart), f.LocalEnd.Set(localEnd), f.ClockStart.Set(clockStart), f.ClockEnd.Set(clockEnd), f.Date.Set(date)}
			if i != 1 {
				assignments = append(assignments, f.MaybePeriod.Set(period))
			}
			sample, err := intervalqueries.Samples.Insert(t.Context(), tx, assignments...)
			if err != nil {
				return err
			}
			samples = append(samples, sample)
		}
		return check(tx, samples)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func intervalOne[V any](t *testing.T, tx *database.Tx, selected expr.Selected[intervalqueries.Sample, V]) V {
	t.Helper()
	result, err := rows.ReadScalar(rows.SelectScalar(rows.From(intervalqueries.Samples.Query().OrderBy(intervalqueries.Samples.ID.Asc()).Limit(1)), selected)).RequireFirst(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
