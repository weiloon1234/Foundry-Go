package temporalqueries_test

import (
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func runTemporal(t *testing.T, check func(*database.Tx, []temporalqueries.Sample) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`SET LOCAL TIME ZONE 'Pacific/Auckland'`,
			`CREATE TABLE temporal_samples (id bigint PRIMARY KEY,millis bigint NOT NULL,"at" timestamptz NOT NULL,instant timestamptz NOT NULL,"date" date NOT NULL,"local" timestamp NOT NULL,"clock" time NOT NULL,maybe_at timestamptz,maybe_date date,maybe_local timestamp,maybe_clock time)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		var samples []temporalqueries.Sample
		for i, text := range []string{"2024-03-09T12:34:56.123456-05:00", "2024-03-10T12:34:56.123456-04:00", "1969-12-31T23:59:59.999500Z", "2019-12-30T00:00:00Z"} {
			at, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return err
			}
			instant, err := temporal.NewDateTime(at)
			if err != nil {
				return err
			}
			date, err := temporal.NewDate(at.Year(), at.Month(), at.Day())
			if err != nil {
				return err
			}
			clock, err := temporal.NewTime(at.Hour(), at.Minute(), at.Second(), at.Nanosecond())
			if err != nil {
				return err
			}
			local, err := temporal.NewLocalDateTime(date, clock)
			if err != nil {
				return err
			}
			f := temporalqueries.Samples
			assignments := []orm.Assignment[temporalqueries.Sample]{orm.NumberKeySet(f.ID, i+1), orm.NumberSet(f.Millis, at.UnixMilli()), orm.TimeSet(f.At, at), orm.TimeSet(f.Instant, instant), orm.TimeSet(f.Date, date), orm.TimeSet(f.Local, local), orm.TimeSet(f.Clock, clock)}
			if i != 1 {
				assignments = append(assignments, orm.NullableTimeSet(f.MaybeAt, at), orm.NullableTimeSet(f.MaybeDate, date), orm.NullableTimeSet(f.MaybeLocal, local), orm.NullableTimeSet(f.MaybeClock, clock))
			}
			sample, err := orm.Insert(t.Context(), tx, temporalqueries.Samples.Table, assignments...)
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

func temporalValues[V any](t *testing.T, tx *database.Tx, expression expr.Selected[temporalqueries.Sample, V], want []V) {
	t.Helper()
	q := temporalqueries.Samples.Query().OrderBy(orm.NumberKeyAsc(temporalqueries.Samples.ID))
	got, err := rows.ReadScalar(rows.SelectScalar(rows.From(q), expression)).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("temporal result", got, want, err)
	}
}
func temporalOne[V any](t *testing.T, tx *database.Tx, expression expr.Selected[temporalqueries.Sample, V]) V {
	t.Helper()
	got, err := rows.ReadScalar(rows.SelectScalar(rows.From(temporalqueries.Samples.Query().Limit(1)), expression)).RequireFirst(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
