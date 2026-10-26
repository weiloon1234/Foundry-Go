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
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
			f := framequeries.Samples
			draft := []orm.Assignment[framequeries.Sample]{f.ID.Set(i + 1), f.Band.Set([]int16{10, 11, 13, 14}[i]), f.Amount.Set(amount), f.At.Set(at), f.Date.Set(date), f.Local.Set(local), f.Clock.Set(clock)}
			if i < 2 {
				draft = append(draft, f.Score.Set([]float32{1, 1.5}[i]))
			}
			if _, err := framequeries.Samples.Insert(t.Context(), tx, draft...); err != nil {
				return err
			}
		}
		return check(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func frameCounts(t *testing.T, tx *database.Tx, w expr.Window[framequeries.Sample], want []int64) {
	t.Helper()
	q := framequeries.Samples.Query()
	got, err := rowset.ReadScalar(rowset.OrderScalar(rowset.SelectScalar(rowset.From(q), expr.Over(expr.Count[framequeries.Sample](), w)), expr.Value(framequeries.Samples.ID).Asc())).All(t.Context(), tx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("window counts", got, want, err)
	}
}

func TestPostgresTypedNumericRanges(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.Samples.Query()
		f := framequeries.Samples
		r := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.ID)).Value())
		frameCounts(t, tx, r.Between(r.Preceding(1), r.CurrentRow()), []int64{1, 2, 2, 2})
		frameCounts(t, tx, r.Desc().Between(r.Preceding(1), r.CurrentRow()), []int64{2, 2, 2, 1})
		frameCounts(t, tx, r.Between(r.Preceding(7), r.Preceding(8)), []int64{0, 0, 0, 0})
		frameCounts(t, tx, r.Between(r.CurrentRow(), r.Following(1)).ExcludeCurrentRow(), []int64{1, 1, 1, 0})
		band := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Band)).Value())
		frameCounts(t, tx, band.Between(band.Preceding(1), band.CurrentRow()), []int64{1, 2, 1, 2})
		money := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Amount)).Value())
		distance, _ := decimal.Parse("0.15")
		frameCounts(t, tx, money.Between(money.Preceding(distance), money.CurrentRow()), []int64{1, 2, 1, 2})
		score := expr.NullableNumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Score)).Value())
		frameCounts(t, tx, score.Between(score.Preceding(0.5), score.CurrentRow()), []int64{1, 2, 2, 2})
		frameCounts(t, tx, score.Between(score.Preceding(0), score.CurrentRow()), []int64{1, 1, 2, 2})
		// Selected numeric calculations retain their concrete distance type.
		bucket := expr.When(expr.ModelPredicate(f.ID.Gt(2)), expr.Param(f.ID, 1)).Else(expr.Param(f.ID, 0))
		computed := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), bucket.Value())
		frameCounts(t, tx, computed.Between(computed.Preceding(0), computed.CurrentRow()), []int64{2, 2, 2, 2})
		// A RANGE can order the grouped rows by a selected ordinary aggregate.
		groupRange := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Count[framequeries.Sample]().Value())
		groupCount := expr.Over(expr.Count[framequeries.Sample](), groupRange.Between(groupRange.Preceding(0), groupRange.CurrentRow()))
		if got, err := rowset.ReadScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), groupCount), expr.Group(bucket))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{2, 2}) {
			t.Fatal("RANGE over grouped counts", got, err)
		}
		sumRange := expr.NullableNumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Sum(f.Amount).Value())
		half, _ := decimal.Parse("0.5")
		sumCount := expr.Over(expr.Count[framequeries.Sample](), sumRange.Between(sumRange.Preceding(half), sumRange.CurrentRow()))
		if got, err := rowset.ReadScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), sumCount), expr.Group(bucket))).All(t.Context(), tx); err != nil || !reflect.DeepEqual(got, []int64{1, 1}) {
			t.Fatal("RANGE over exact nullable aggregate", got, err)
		}
		return nil
	})
}

func TestPostgresTypedTemporalRanges(t *testing.T) {
	runSamples(t, nil, func(tx *database.Tx) error {
		q := framequeries.Samples.Query()
		f := framequeries.Samples
		r := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.At)).Value())
		day, _ := temporal.Elapsed(24 * time.Hour)
		frameCounts(t, tx, r.Between(r.Preceding(temporal.Days(1)), r.CurrentRow()), []int64{1, 1, 2, 2})
		frameCounts(t, tx, r.Between(r.Preceding(day), r.CurrentRow()), []int64{1, 2, 2, 2})
		local := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Local)).Value())
		frameCounts(t, tx, local.Between(local.Preceding(temporal.Days(1)), local.CurrentRow()), []int64{1, 1, 2, 2})
		date := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Date)).Value())
		frameCounts(t, tx, date.Between(date.Preceding(temporal.Days(1)), date.CurrentRow()), []int64{1, 2, 3, 3})
		maybeDate := expr.When(expr.ModelPredicate(f.ID.Gt(2)), expr.Value(f.Date)).ElseNull()
		nullable := expr.NullableTemporalRange(expr.WindowFor(rowset.ModelScope(q)), maybeDate.Value())
		frameCounts(t, tx, nullable.Between(nullable.Preceding(temporal.Days(1)), nullable.CurrentRow()), []int64{2, 2, 2, 2})
		clock := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Clock)).Value())
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
		q := framequeries.Samples.Query()
		r := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(framequeries.Samples.Date)).Value())
		frameCounts(t, tx, r.Between(r.Preceding(temporal.Months(1)), r.CurrentRow()), []int64{1, 2, 3, 3})
		return nil
	})
}

func TestRangeFailuresBeforeExecution(t *testing.T) {
	q := framequeries.Samples.Query()
	f := framequeries.Samples
	r := expr.NumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.ID)).Value())
	score := expr.NullableNumericRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Score)).Value())
	clock := expr.TemporalRange(expr.WindowFor(rowset.ModelScope(q)), expr.Row(expr.Value(f.Clock)).Value())
	for _, w := range []expr.Window[framequeries.Sample]{
		r.Between(r.Preceding(-1), r.CurrentRow()),
		r.Between(r.Preceding(1), r.CurrentRow()).OrderBy(expr.Value(f.Amount).Asc()),
		score.Between(score.Preceding(float32(math.Inf(1))), score.CurrentRow()),
		clock.Between(clock.Preceding(temporal.Days(1)), clock.CurrentRow()),
	} {
		if _, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.Limit(0)), expr.RowNumber(w))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid RANGE reached executor", err)
		}
	}
}
