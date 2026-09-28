package framequeries_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/framequeries"
	"foundry.test/consumer/internal/queryfixture"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func runSamples(t *testing.T, instants []string, check func(*database.Tx) error) {
	t.Helper()
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	if instants == nil {
		instants = []string{"2024-03-09T11:30:00-05:00", "2024-03-10T12:00:00-04:00", "2024-03-11T11:30:00-04:00", "2024-03-11T12:30:00-04:00"}
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `SET LOCAL TIME ZONE 'America/New_York'`, `CREATE TABLE frame_samples (id bigint PRIMARY KEY,band smallint NOT NULL,amount numeric NOT NULL,score real,"at" timestamptz NOT NULL,"date" date NOT NULL,"local" timestamp NOT NULL,"clock" time NOT NULL)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		for i, text := range instants {
			at, err := time.Parse(time.RFC3339, text)
			if err != nil {
				return err
			}
			date, err := temporal.NewDate(at.Year(), at.Month(), at.Day())
			if err != nil {
				return err
			}
			clock, err := temporal.NewTime(at.Hour(), at.Minute(), at.Second(), 0)
			if err != nil {
				return err
			}
			local, err := temporal.NewLocalDateTime(date, clock)
			if err != nil {
				return err
			}
			amount, err := decimal.Parse([]string{"9007199254740993.0", "9007199254740993.1", "9007199254740993.3", "9007199254740993.4"}[i])
			if err != nil {
				return err
			}
			draft := framequeries.SampleDraft{}.SetID(i + 1).SetBand([]int16{10, 11, 13, 14}[i]).SetAmount(amount).SetAt(at).SetDate(date).SetLocal(local).SetClock(clock)
			if i < 2 {
				draft = draft.SetScore([]float32{1, 1.5}[i])
			}
			if _, err := framequeries.QueryFrameSamples().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		return check(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func frameCounts(t *testing.T, tx *database.Tx, w query.Window[framequeries.Sample], want []int64) {
	t.Helper()
	q := framequeries.QueryFrameSamples()
	got, err := query.SelectValue(q, query.Count[framequeries.Sample]().Over(w)).OrderBy(framequeries.SampleFields().ID.Asc()).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("window counts", got, want, err)
	}
}

func TestPostgresTypedNumericRanges(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.QueryFrameSamples()
		f := framequeries.SampleFields()
		r := query.NumericRange(query.WindowFor(q), f.ID.Value())
		frameCounts(t, tx, r.Between(r.Preceding(1), r.CurrentRow()), []int64{1, 2, 2, 2})
		frameCounts(t, tx, r.Desc().Between(r.Preceding(1), r.CurrentRow()), []int64{2, 2, 2, 1})
		frameCounts(t, tx, r.Between(r.Preceding(7), r.Preceding(8)), []int64{0, 0, 0, 0})
		frameCounts(t, tx, r.Between(r.CurrentRow(), r.Following(1)).ExcludeCurrentRow(), []int64{1, 1, 1, 0})
		band := query.NumericRange(query.WindowFor(q), f.Band.Value())
		frameCounts(t, tx, band.Between(band.Preceding(1), band.CurrentRow()), []int64{1, 2, 1, 2})
		money := query.NumericRange(query.WindowFor(q), f.Amount.Value())
		distance, _ := decimal.Parse("0.15")
		frameCounts(t, tx, money.Between(money.Preceding(distance), money.CurrentRow()), []int64{1, 2, 1, 2})
		score := query.NullableNumericRange(query.WindowFor(q), f.Score.Value())
		frameCounts(t, tx, score.Between(score.Preceding(0.5), score.CurrentRow()), []int64{1, 2, 2, 2})
		frameCounts(t, tx, score.Between(score.Preceding(0), score.CurrentRow()), []int64{1, 1, 2, 2})
		// Selected numeric calculations retain their concrete distance type.
		bucket := query.When(f.ID.Gt(2), f.ID.Param(1)).Else(f.ID.Param(0))
		computed := query.NumericRange(query.WindowFor(q), bucket.Value())
		frameCounts(t, tx, computed.Between(computed.Preceding(0), computed.CurrentRow()), []int64{2, 2, 2, 2})
		// A RANGE can order the grouped rows by a selected ordinary aggregate.
		groupRange := query.NumericRange(query.WindowFor(q), query.Count[framequeries.Sample]().Value())
		groupCount := query.Count[framequeries.Sample]().Over(groupRange.Between(groupRange.Preceding(0), groupRange.CurrentRow()))
		if got, err := query.SelectValue(q, groupCount).GroupBy(bucket.Group()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{2, 2}) {
			t.Fatal("RANGE over grouped counts", got, err)
		}
		sumRange := query.NullableNumericRange(query.WindowFor(q), f.Amount.Sum().Value())
		half, _ := decimal.Parse("0.5")
		sumCount := query.Count[framequeries.Sample]().Over(sumRange.Between(sumRange.Preceding(half), sumRange.CurrentRow()))
		if got, err := query.SelectValue(q, sumCount).GroupBy(bucket.Group()).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("RANGE over exact nullable aggregate", got, err)
		}
		return nil
	})
}

func TestPostgresTypedTemporalRanges(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.QueryFrameSamples()
		f := framequeries.SampleFields()
		r := query.TemporalRange(query.WindowFor(q), f.At.Value())
		day, _ := temporal.Elapsed(24 * time.Hour)
		frameCounts(t, tx, r.Between(r.Preceding(temporal.Days(1)), r.CurrentRow()), []int64{1, 1, 2, 2})
		frameCounts(t, tx, r.Between(r.Preceding(day), r.CurrentRow()), []int64{1, 2, 2, 2})
		local := query.TemporalRange(query.WindowFor(q), f.Local.Value())
		frameCounts(t, tx, local.Between(local.Preceding(temporal.Days(1)), local.CurrentRow()), []int64{1, 1, 2, 2})
		date := query.TemporalRange(query.WindowFor(q), f.Date.Value())
		frameCounts(t, tx, date.Between(date.Preceding(temporal.Days(1)), date.CurrentRow()), []int64{1, 2, 3, 3})
		maybeDate := query.When(f.ID.Gt(2), f.Date).ElseNull()
		nullable := query.NullableTemporalRange(query.WindowFor(q), maybeDate.Value())
		frameCounts(t, tx, nullable.Between(nullable.Preceding(temporal.Days(1)), nullable.CurrentRow()), []int64{2, 2, 2, 2})
		clock := query.TemporalRange(query.WindowFor(q), f.Clock.Value())
		halfHour, _ := temporal.Elapsed(30 * time.Minute)
		frameCounts(t, tx, clock.Between(clock.Preceding(halfHour), clock.CurrentRow()), []int64{2, 3, 2, 2})
		// Explicit component signs preserve meaning under different interval styles.
		if _, err := tx.Exec(t.Context(), `SET LOCAL intervalstyle TO 'sql_standard'`); err != nil {
			return err
		}
		frameCounts(t, tx, r.Between(r.Preceding(day), r.CurrentRow()), []int64{1, 2, 2, 2})
		return nil
	})
	runSamples(t, []string{"2024-02-29T12:00:00Z", "2024-03-30T12:00:00Z", "2024-03-31T12:00:00Z", "2024-04-30T12:00:00Z"}, func(tx *database.Tx) error {
		q := framequeries.QueryFrameSamples()
		r := query.TemporalRange(query.WindowFor(q), framequeries.SampleFields().Date.Value())
		frameCounts(t, tx, r.Between(r.Preceding(temporal.Months(1)), r.CurrentRow()), []int64{1, 2, 3, 3})
		return nil
	})
}

func TestRangeFailuresBeforeExecution(t *testing.T) {
	q := framequeries.QueryFrameSamples()
	f := framequeries.SampleFields()
	r := query.NumericRange(query.WindowFor(q), f.ID.Value())
	score := query.NullableNumericRange(query.WindowFor(q), f.Score.Value())
	clock := query.TemporalRange(query.WindowFor(q), f.Clock.Value())
	for _, w := range []query.Window[framequeries.Sample]{
		r.Between(r.Preceding(-1), r.CurrentRow()),
		r.Between(r.Preceding(1), r.CurrentRow()).OrderBy(f.Amount.Asc()),
		score.Between(score.Preceding(float32(math.Inf(1))), score.CurrentRow()),
		clock.Between(clock.Preceding(temporal.Days(1)), clock.CurrentRow()),
	} {
		if _, err := query.SelectValue(q.Limit(0), query.RowNumber(w)).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid RANGE reached executor", err)
		}
	}
}
