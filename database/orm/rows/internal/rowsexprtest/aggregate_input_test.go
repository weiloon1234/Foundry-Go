package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type aggregateWriteInput struct{ Text string }
type inputAggregateRecord struct {
	ID          int64
	Number      int64
	MaybeNumber value.Nullable[int64]
	Approx      float64
	MaybeApprox value.Nullable[float64]
	Time        time.Time
	MaybeTime   value.Nullable[time.Time]
	Period      temporal.Interval
	MaybePeriod value.Nullable[temporal.Interval]
	Text        aggregateName
	MaybeText   value.Nullable[aggregateName]
}
type storedAggregateFields struct {
	orm.Table[inputAggregateRecord, int64]
	Number      orm.Number[inputAggregateRecord, int64]
	MaybeNumber orm.NullableNumber[inputAggregateRecord, int64]
	Approx      orm.Number[inputAggregateRecord, float64]
	MaybeApprox orm.NullableNumber[inputAggregateRecord, float64]
	Time        orm.Time[inputAggregateRecord, time.Time]
	MaybeTime   orm.NullableTime[inputAggregateRecord, time.Time]
	Period      orm.Interval[inputAggregateRecord]
	MaybePeriod orm.NullableInterval[inputAggregateRecord]
	Text        orm.Text[inputAggregateRecord, aggregateName]
	MaybeText   orm.NullableText[inputAggregateRecord, aggregateName]
}
type inputAggregateFields struct {
	orm.Table[inputAggregateRecord, int64]
	Number      orm.InputNumber[inputAggregateRecord, aggregateWriteInput, int64]
	MaybeNumber orm.NullableInputNumber[inputAggregateRecord, aggregateWriteInput, int64]
	Approx      orm.InputNumber[inputAggregateRecord, aggregateWriteInput, float64]
	MaybeApprox orm.NullableInputNumber[inputAggregateRecord, aggregateWriteInput, float64]
	Time        orm.InputTime[inputAggregateRecord, aggregateWriteInput, time.Time]
	MaybeTime   orm.NullableInputTime[inputAggregateRecord, aggregateWriteInput, time.Time]
	Period      orm.InputInterval[inputAggregateRecord, aggregateWriteInput]
	MaybePeriod orm.NullableInputInterval[inputAggregateRecord, aggregateWriteInput]
	Text        orm.InputText[inputAggregateRecord, aggregateWriteInput, aggregateName]
	MaybeText   orm.NullableInputText[inputAggregateRecord, aggregateWriteInput, aggregateName]
}

// Reading aggregates must never call the mutation-input transform.
func rejectAggregateTransform[V any](aggregateWriteInput) (V, error) {
	panic("aggregate read invoked a write transform")
}
func declareStoredAggregateFields() storedAggregateFields {
	return orm.Define("input_aggregate_records", func(b orm.Builder[inputAggregateRecord], m *inputAggregateRecord) storedAggregateFields {
		return storedAggregateFields{Table: orm.TableOf(b, &m.ID),
			Number:      orm.NumberOf(b, &m.Number),
			MaybeNumber: orm.NullableNumberOf(b, &m.MaybeNumber),
			Approx:      orm.NumberOf(b, &m.Approx),
			MaybeApprox: orm.NullableNumberOf(b, &m.MaybeApprox),
			Time:        orm.TimeOf(b, &m.Time),
			MaybeTime:   orm.NullableTimeOf(b, &m.MaybeTime),
			Period:      orm.IntervalOf(b, &m.Period),
			MaybePeriod: orm.NullableIntervalOf(b, &m.MaybePeriod),
			Text:        orm.TextOf(b, &m.Text),
			MaybeText:   orm.NullableTextOf(b, &m.MaybeText),
		}
	})
}
func declareInputAggregateFields() inputAggregateFields {
	return orm.Define("input_aggregate_records", func(b orm.Builder[inputAggregateRecord], m *inputAggregateRecord) inputAggregateFields {
		return inputAggregateFields{Table: orm.TableOf(b, &m.ID),
			Number:      orm.InputNumberOf(b, &m.Number, rejectAggregateTransform[int64]),
			MaybeNumber: orm.NullableInputNumberOf(b, &m.MaybeNumber, rejectAggregateTransform[int64]),
			Approx:      orm.InputNumberOf(b, &m.Approx, rejectAggregateTransform[float64]),
			MaybeApprox: orm.NullableInputNumberOf(b, &m.MaybeApprox, rejectAggregateTransform[float64]),
			Time:        orm.InputTimeOf(b, &m.Time, rejectAggregateTransform[time.Time]),
			MaybeTime:   orm.NullableInputTimeOf(b, &m.MaybeTime, rejectAggregateTransform[time.Time]),
			Period:      orm.InputIntervalOf(b, &m.Period, rejectAggregateTransform[temporal.Interval]),
			MaybePeriod: orm.NullableInputIntervalOf(b, &m.MaybePeriod, rejectAggregateTransform[temporal.Interval]),
			Text:        orm.InputTextOf(b, &m.Text, rejectAggregateTransform[aggregateName]),
			MaybeText:   orm.NullableInputTextOf(b, &m.MaybeText, rejectAggregateTransform[aggregateName]),
		}
	})
}

func TestOrderedInputAggregatesUseStoredValues(t *testing.T) {
	stored, input := declareStoredAggregateFields(), declareInputAggregateFields()
	if err := orm.Check(stored, input); err != nil {
		t.Fatal(err)
	}
	storedSource, inputSource := rows.FromModel(stored.Query()), rows.FromModel(input.Query())
	storedAlias, inputAlias := rows.As[left](stored.Query(), "source"), rows.As[left](input.Query(), "source")
	t.Run("Number/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.InputNumberExpr(input.Number)).Value(), expr.Min(orm.NumberExpr(stored.Number)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Number)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Number)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.InputNumberExpr(input.Number)).Value(), expr.Max(orm.NumberExpr(stored.Number)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Number)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Number)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/Sum", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Sum(orm.InputNumberExpr(input.Number)).Value(), expr.Sum(orm.NumberExpr(stored.Number)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/SumAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Number)).Value(), expr.SumAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Number)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/Avg", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Avg(orm.InputNumberExpr(input.Number)).Value(), expr.Avg(orm.NumberExpr(stored.Number)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/AvgAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Number)).Value(), expr.AvgAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Number)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.Min(orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.Max(orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/Sum", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Sum(orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.Sum(orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/SumAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.SumAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/Avg", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Avg(orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.Avg(orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/AvgAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeNumber)).Value(), expr.AvgAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeNumber)).Value(), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Approx/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.InputNumberExpr(input.Approx)).Value(), expr.Min(orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Approx)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.InputNumberExpr(input.Approx)).Value(), expr.Max(orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Approx)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/SumFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.SumFloat(orm.InputNumberExpr(input.Approx)).Value(), expr.SumFloat(orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/SumFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumFloatAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Approx)).Value(), expr.SumFloatAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/AvgFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AvgFloat(orm.InputNumberExpr(input.Approx)).Value(), expr.AvgFloat(orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/AvgFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgFloatAt(rows.Scope(inputAlias), orm.InputNumberExpr(input.Approx)).Value(), expr.AvgFloatAt(rows.Scope(storedAlias), orm.NumberExpr(stored.Approx)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.Min(orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.Max(orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/SumFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.SumFloat(orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.SumFloat(orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/SumFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumFloatAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.SumFloatAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/AvgFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AvgFloat(orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.AvgFloat(orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/AvgFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgFloatAt(rows.Scope(inputAlias), orm.NullableInputNumberExpr(input.MaybeApprox)).Value(), expr.AvgFloatAt(rows.Scope(storedAlias), orm.NullableNumberExpr(stored.MaybeApprox)).Value(), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Time/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.InputTimeExpr(input.Time)).Value(), expr.Min(orm.TimeExpr(stored.Time)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.InputTimeExpr(input.Time)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.TimeExpr(stored.Time)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.InputTimeExpr(input.Time)).Value(), expr.Max(orm.TimeExpr(stored.Time)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.InputTimeExpr(input.Time)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.TimeExpr(stored.Time)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.NullableInputTimeExpr(input.MaybeTime)).Value(), expr.Min(orm.NullableTimeExpr(stored.MaybeTime)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.NullableInputTimeExpr(input.MaybeTime)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NullableTimeExpr(stored.MaybeTime)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.NullableInputTimeExpr(input.MaybeTime)).Value(), expr.Max(orm.NullableTimeExpr(stored.MaybeTime)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.NullableInputTimeExpr(input.MaybeTime)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NullableTimeExpr(stored.MaybeTime)).Value(), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Period/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.InputIntervalExpr(input.Period)).Value(), expr.Min(orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.InputIntervalExpr(input.Period)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.InputIntervalExpr(input.Period)).Value(), expr.Max(orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.InputIntervalExpr(input.Period)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/SumInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.SumInterval(orm.InputIntervalExpr(input.Period)).Value(), expr.SumInterval(orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/SumIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumIntervalAt(rows.Scope(inputAlias), orm.InputIntervalExpr(input.Period)).Value(), expr.SumIntervalAt(rows.Scope(storedAlias), orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/AvgInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AvgInterval(orm.InputIntervalExpr(input.Period)).Value(), expr.AvgInterval(orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/AvgIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgIntervalAt(rows.Scope(inputAlias), orm.InputIntervalExpr(input.Period)).Value(), expr.AvgIntervalAt(rows.Scope(storedAlias), orm.IntervalExpr(stored.Period)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.Min(orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.Max(orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/SumInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.SumInterval(orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.SumInterval(orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/SumIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.SumIntervalAt(rows.Scope(inputAlias), orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.SumIntervalAt(rows.Scope(storedAlias), orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/AvgInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AvgInterval(orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.AvgInterval(orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/AvgIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AvgIntervalAt(rows.Scope(inputAlias), orm.NullableInputIntervalExpr(input.MaybePeriod)).Value(), expr.AvgIntervalAt(rows.Scope(storedAlias), orm.NullableIntervalExpr(stored.MaybePeriod)).Value(), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Text/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.InputTextExpr(input.Text)).Value(), expr.Min(orm.TextExpr(stored.Text)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.InputTextExpr(input.Text)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.TextExpr(stored.Text)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.InputTextExpr(input.Text)).Value(), expr.Max(orm.TextExpr(stored.Text)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.InputTextExpr(input.Text)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.TextExpr(stored.Text)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Min(orm.NullableInputTextExpr(input.MaybeText)).Value(), expr.Min(orm.NullableTextExpr(stored.MaybeText)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MinAt(rows.Scope(inputAlias), orm.NullableInputTextExpr(input.MaybeText)).Value(), expr.MinAt(rows.Scope(storedAlias), orm.NullableTextExpr(stored.MaybeText)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.Max(orm.NullableInputTextExpr(input.MaybeText)).Value(), expr.Max(orm.NullableTextExpr(stored.MaybeText)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.MaxAt(rows.Scope(inputAlias), orm.NullableInputTextExpr(input.MaybeText)).Value(), expr.MaxAt(rows.Scope(storedAlias), orm.NullableTextExpr(stored.MaybeText)).Value(), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})

	// Ordered input columns also retain the pre-integration comparison capability.
	comparison := expr.GreaterColumn(orm.InputNumberExpr(input.Number), orm.InputNumberExpr(input.Number))
	if _, err := orm.Count(t.Context(), openTest(t, &driverState{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}), expr.Where(input.Query(), comparison)); err != nil {
		t.Fatal(err)
	}
}

func checkInputAggregate[S, V any](t *testing.T, inputSource, storedSource rows.SelectionSource[S], input, stored expr.Selected[S, value.Nullable[V]], c codec.Codec[V], raw driver.Value, want V) {
	t.Helper()
	iq := expressionQuery(inputSource, input, codec.Nullable(c))
	sq := expressionQuery(storedSource, stored, codec.Nullable(c))
	actual, expected := expressionSQL(t, iq), expressionSQL(t, sq)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("input aggregate differs from stored aggregate:\n%#v\n%#v", actual, expected)
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {raw}}}
	got, err := rows.Read(iq).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
		t.Fatal(got, err)
	}
	present, ok := got[1].Value.Get()
	if !ok || !reflect.DeepEqual(present, want) {
		t.Fatalf("stored result = %#v; want %#v", got[1].Value, want)
	}
}
