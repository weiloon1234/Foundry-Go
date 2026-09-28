package temporalqueries_test

import (
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/temporalqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
			draft := temporalqueries.SampleDraft{}.SetID(i + 1).SetMillis(at.UnixMilli()).SetAt(at).SetInstant(instant).SetDate(date).SetLocal(local).SetClock(clock)
			if i != 1 {
				draft = draft.SetMaybeAt(at).SetMaybeDate(date).SetMaybeLocal(local).SetMaybeClock(clock)
			}
			sample, err := temporalqueries.QueryTemporalSamples().Create(t.Context(), tx, draft)
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

func temporalValues[V any](t *testing.T, tx *database.Tx, expression query.Expression[temporalqueries.Sample, V], want []V) {
	t.Helper()
	q := temporalqueries.QueryTemporalSamples().OrderBy(temporalqueries.SampleFields().ID.Asc())
	got, err := query.SelectValue(q, expression).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("temporal result", got, want, err)
	}
}
func temporalOne[V any](t *testing.T, tx *database.Tx, expression query.Expression[temporalqueries.Sample, V]) V {
	t.Helper()
	got, err := query.SelectValue(temporalqueries.QueryTemporalSamples().Limit(1), expression).RequireFirst(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
