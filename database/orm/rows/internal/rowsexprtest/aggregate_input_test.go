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
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.Number)), expr.AsSelected(expr.Min(stored.Number)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.Number)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.Number)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.Number)), expr.AsSelected(expr.Max(stored.Number)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.Number)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.Number)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("Number/Sum", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Sum(input.Number)), expr.AsSelected(expr.Sum(stored.Number)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/SumAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumAt(rows.Scope(inputAlias), input.Number)), expr.AsSelected(expr.SumAt(rows.Scope(storedAlias), stored.Number)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/Avg", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Avg(input.Number)), expr.AsSelected(expr.Avg(stored.Number)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Number/AvgAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgAt(rows.Scope(inputAlias), input.Number)), expr.AsSelected(expr.AvgAt(rows.Scope(storedAlias), stored.Number)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.MaybeNumber)), expr.AsSelected(expr.Min(stored.MaybeNumber)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.MaybeNumber)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.MaybeNumber)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.MaybeNumber)), expr.AsSelected(expr.Max(stored.MaybeNumber)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.MaybeNumber)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.MaybeNumber)), codec.Signed[int64](), int64(7), int64(7))
	})
	t.Run("MaybeNumber/Sum", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Sum(input.MaybeNumber)), expr.AsSelected(expr.Sum(stored.MaybeNumber)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/SumAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumAt(rows.Scope(inputAlias), input.MaybeNumber)), expr.AsSelected(expr.SumAt(rows.Scope(storedAlias), stored.MaybeNumber)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/Avg", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Avg(input.MaybeNumber)), expr.AsSelected(expr.Avg(stored.MaybeNumber)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("MaybeNumber/AvgAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgAt(rows.Scope(inputAlias), input.MaybeNumber)), expr.AsSelected(expr.AvgAt(rows.Scope(storedAlias), stored.MaybeNumber)), codec.Decimal(), "7", decimal.FromInt64(7))
	})
	t.Run("Approx/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.Approx)), expr.AsSelected(expr.Min(stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.Approx)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.Approx)), expr.AsSelected(expr.Max(stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.Approx)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/SumFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.SumFloat(input.Approx)), expr.AsSelected(expr.SumFloat(stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/SumFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumFloatAt(rows.Scope(inputAlias), input.Approx)), expr.AsSelected(expr.SumFloatAt(rows.Scope(storedAlias), stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/AvgFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.AvgFloat(input.Approx)), expr.AsSelected(expr.AvgFloat(stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Approx/AvgFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgFloatAt(rows.Scope(inputAlias), input.Approx)), expr.AsSelected(expr.AvgFloatAt(rows.Scope(storedAlias), stored.Approx)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.MaybeApprox)), expr.AsSelected(expr.Min(stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.MaybeApprox)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.MaybeApprox)), expr.AsSelected(expr.Max(stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.MaybeApprox)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/SumFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.SumFloat(input.MaybeApprox)), expr.AsSelected(expr.SumFloat(stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/SumFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumFloatAt(rows.Scope(inputAlias), input.MaybeApprox)), expr.AsSelected(expr.SumFloatAt(rows.Scope(storedAlias), stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/AvgFloat", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.AvgFloat(input.MaybeApprox)), expr.AsSelected(expr.AvgFloat(stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("MaybeApprox/AvgFloatAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgFloatAt(rows.Scope(inputAlias), input.MaybeApprox)), expr.AsSelected(expr.AvgFloatAt(rows.Scope(storedAlias), stored.MaybeApprox)), codec.Float[float64](), float64(7.5), float64(7.5))
	})
	t.Run("Time/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.Time)), expr.AsSelected(expr.Min(stored.Time)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.Time)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.Time)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.Time)), expr.AsSelected(expr.Max(stored.Time)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Time/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.Time)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.Time)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.MaybeTime)), expr.AsSelected(expr.Min(stored.MaybeTime)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.MaybeTime)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.MaybeTime)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.MaybeTime)), expr.AsSelected(expr.Max(stored.MaybeTime)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("MaybeTime/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.MaybeTime)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.MaybeTime)), codec.Time(), time.Unix(7, 0).UTC(), time.Unix(7, 0).UTC())
	})
	t.Run("Period/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.Period)), expr.AsSelected(expr.Min(stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.Period)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.Period)), expr.AsSelected(expr.Max(stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.Period)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/SumInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.SumInterval(input.Period)), expr.AsSelected(expr.SumInterval(stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/SumIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumIntervalAt(rows.Scope(inputAlias), input.Period)), expr.AsSelected(expr.SumIntervalAt(rows.Scope(storedAlias), stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/AvgInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.AvgInterval(input.Period)), expr.AsSelected(expr.AvgInterval(stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Period/AvgIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgIntervalAt(rows.Scope(inputAlias), input.Period)), expr.AsSelected(expr.AvgIntervalAt(rows.Scope(storedAlias), stored.Period)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.MaybePeriod)), expr.AsSelected(expr.Min(stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.MaybePeriod)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.MaybePeriod)), expr.AsSelected(expr.Max(stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.MaybePeriod)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/SumInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.SumInterval(input.MaybePeriod)), expr.AsSelected(expr.SumInterval(stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/SumIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.SumIntervalAt(rows.Scope(inputAlias), input.MaybePeriod)), expr.AsSelected(expr.SumIntervalAt(rows.Scope(storedAlias), stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/AvgInterval", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.AvgInterval(input.MaybePeriod)), expr.AsSelected(expr.AvgInterval(stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("MaybePeriod/AvgIntervalAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.AvgIntervalAt(rows.Scope(inputAlias), input.MaybePeriod)), expr.AsSelected(expr.AvgIntervalAt(rows.Scope(storedAlias), stored.MaybePeriod)), codec.Interval(), "2 days", temporal.Days(2))
	})
	t.Run("Text/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.Text)), expr.AsSelected(expr.Min(stored.Text)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.Text)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.Text)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.Text)), expr.AsSelected(expr.Max(stored.Text)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("Text/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.Text)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.Text)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/Min", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Min(input.MaybeText)), expr.AsSelected(expr.Min(stored.MaybeText)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/MinAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MinAt(rows.Scope(inputAlias), input.MaybeText)), expr.AsSelected(expr.MinAt(rows.Scope(storedAlias), stored.MaybeText)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/Max", func(t *testing.T) {
		checkInputAggregate(t, inputSource, storedSource, expr.AsSelected(expr.Max(input.MaybeText)), expr.AsSelected(expr.Max(stored.MaybeText)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})
	t.Run("MaybeText/MaxAt", func(t *testing.T) {
		checkInputAggregate(t, inputAlias, storedAlias, expr.AsSelected(expr.MaxAt(rows.Scope(inputAlias), input.MaybeText)), expr.AsSelected(expr.MaxAt(rows.Scope(storedAlias), stored.MaybeText)), codec.String[aggregateName](), "Ada", aggregateName("Ada"))
	})

	// Ordered input columns also retain the pre-integration comparison capability.
	comparison := expr.GreaterColumn(input.Number, input.Number)
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
