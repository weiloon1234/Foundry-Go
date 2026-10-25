package rows

import (
	"database/sql"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

type aggregateRowFields struct {
	Row[aggregateRecord]
	ID          Field[aggregateRecord, int64]
	Exact       Field[aggregateRecord, int64]
	MaybeExact  Field[aggregateRecord, value.Nullable[int64]]
	Approx      Field[aggregateRecord, float64]
	MaybeApprox Field[aggregateRecord, value.Nullable[float64]]
	Period      Field[aggregateRecord, temporal.Interval]
	MaybePeriod Field[aggregateRecord, value.Nullable[temporal.Interval]]
	Name        Field[aggregateRecord, aggregateName]
	MaybeName   Field[aggregateRecord, value.Nullable[aggregateName]]
}

func derivedAggregateSource() (Aliased[left, aggregateRecord], aggregateRowFields) {
	d := declareAggregateRecords()
	r := Define(func(b Builder[aggregateRecord], v *aggregateRecord) aggregateRowFields {
		return aggregateRowFields{Row: b.Row(),
			ID:          NumberOf(b, &v.ID),
			Exact:       NumberOf(b, &v.Exact),
			MaybeExact:  NullableNumberOf(b, &v.MaybeExact),
			Approx:      NumberOf(b, &v.Approx),
			MaybeApprox: NullableNumberOf(b, &v.MaybeApprox),
			Period:      FieldOf(b, &v.Period, orm.Codec(codec.Interval())),
			MaybePeriod: NullableFieldOf(b, &v.MaybePeriod, orm.Codec(codec.Interval())),
			Name:        TextOf(b, &v.Name),
			MaybeName:   NullableTextOf(b, &v.MaybeName),
		}
	})
	q := Select(From(d.Query()), r.Row,
		Map(r.ID, expr.Value(d.Key())),
		Map(r.Exact, expr.Value(d.Exact)),
		Map(r.MaybeExact, expr.Value(d.MaybeExact)),
		Map(r.Approx, expr.Value(d.Approx)),
		Map(r.MaybeApprox, expr.Value(d.MaybeApprox)),
		Map(r.Period, expr.Value(d.Period)),
		Map(r.MaybePeriod, expr.Value(d.MaybePeriod)),
		Map(r.Name, expr.Value(d.Name)),
		Map(r.MaybeName, expr.Value(d.MaybeName)),
	)
	return AsRecord[left](RecordOf(q), "derived"), r
}

func TestDerivedAggregateFamilies(t *testing.T) {
	a, r := derivedAggregateSource()
	s := Scope(a)
	t.Run("SumRow", func(t *testing.T) {
		aggregate := expr.SumRow(At(s, r.Exact))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumNullableRow", func(t *testing.T) {
		aggregate := expr.SumNullableRow(At(s, r.MaybeExact))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgRow", func(t *testing.T) {
		aggregate := expr.AvgRow(At(s, r.Exact))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgNullableRow", func(t *testing.T) {
		aggregate := expr.AvgNullableRow(At(s, r.MaybeExact))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumFloatRow", func(t *testing.T) {
		aggregate := expr.SumFloatRow(At(s, r.Approx))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumFloatNullableRow", func(t *testing.T) {
		aggregate := expr.SumFloatNullableRow(At(s, r.MaybeApprox))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgFloatRow", func(t *testing.T) {
		aggregate := expr.AvgFloatRow(At(s, r.Approx))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgFloatNullableRow", func(t *testing.T) {
		aggregate := expr.AvgFloatNullableRow(At(s, r.MaybeApprox))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumIntervalRow", func(t *testing.T) {
		aggregate := expr.SumIntervalRow(At(s, r.Period))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumIntervalNullableRow", func(t *testing.T) {
		aggregate := expr.SumIntervalNullableRow(At(s, r.MaybePeriod))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgIntervalRow", func(t *testing.T) {
		aggregate := expr.AvgIntervalRow(At(s, r.Period))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgIntervalNullableRow", func(t *testing.T) {
		aggregate := expr.AvgIntervalNullableRow(At(s, r.MaybePeriod))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MinRow", func(t *testing.T) {
		aggregate := expr.MinRow(At(s, r.Name))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MinNullableRow", func(t *testing.T) {
		aggregate := expr.MinNullableRow(At(s, r.MaybeName))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MaxRow", func(t *testing.T) {
		aggregate := expr.MaxRow(At(s, r.Name))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MaxNullableRow", func(t *testing.T) {
		aggregate := expr.MaxNullableRow(At(s, r.MaybeName))
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	for _, aggregate := range []expr.OrderedAggregate[Alias[left, aggregateRecord], int64]{expr.CountRow(At(s, r.Exact)), expr.CountDistinctRow(At(s, r.MaybeExact))} {
		if _, err := CompileRead(ReadScalar(SelectScalar(a, aggregate.Value()))); err != nil {
			t.Fatal(err)
		}
	}
	// Nullable min/max retain the codec and accept present fallback values.
	minimum := expr.MinNullableRow(At(s, r.MaybeName))
	_, column := expr.FoundryAggregate(minimum, sqlowner.Expressions())
	var decoded value.Nullable[aggregateName]
	scanner := column.Scan(&decoded).(sql.Scanner)
	if err := scanner.Scan("Ada"); err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded.Get(); !ok || got != "Ada" {
		t.Fatal(decoded)
	}
	if err := scanner.Scan(nil); err != nil || !decoded.IsNull() {
		t.Fatal(decoded, err)
	}
	fallback := expr.CoalesceValue(minimum.Value(), minimum.Param(aggregateName("fallback")).Value())
	if _, err := CompileRead(ReadScalar(SelectScalar(a, fallback))); err != nil {
		t.Fatal(err)
	}
	if got, err := expr.FoundrySelectedColumn(minimum.Param(aggregateName("fallback")).Value(), sqlowner.Expressions()).Bind(aggregateName("fallback")); err != nil || got != "fallback" {
		t.Fatal(got, err)
	}
}

type numericRowEnum int

func (numericRowEnum) EnumDescriptor() enum.Descriptor[numericRowEnum] {
	return enum.Describe("rows_test", "NumericRowEnum", enum.Case[numericRowEnum]{Name: "One", Value: 1})
}
func TestDerivedAggregatesRejectEnumMetadataAndWrongOwners(t *testing.T) {
	a, r := derivedAggregateSource()
	other, foreign := derivedAggregateSource()
	foreignSum := expr.SumRow(At(Scope(other), foreign.Exact))
	if _, err := CompileRead(ReadScalar(SelectScalar(a, foreignSum.Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign aggregate accepted", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(a, expr.SumRow(At(Scope(a), r.Exact).Param(1)).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("parameter accepted as a stored field", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(a, expr.SumRow(expr.RowExpression[Alias[left, aggregateRecord], int64]{}).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero aggregate accepted", err)
	}
	enumRow := Define(func(b Builder[cell[numericRowEnum]], v *cell[numericRowEnum]) struct {
		Row[cell[numericRowEnum]]
		Value Field[cell[numericRowEnum], numericRowEnum]
	} {
		return struct {
			Row[cell[numericRowEnum]]
			Value Field[cell[numericRowEnum], numericRowEnum]
		}{b.Row(), EnumOf(b, &v.Value)}
	})
	// Feed the concrete declaration metadata through the same sealed rows.At path.
	enumSource := AsRecord[left](RecordOf(Select(a, enumRow, MapSelected(enumRow.Value, expr.Raw[Alias[left, aggregateRecord], numericRowEnum]("1")))), "enum_input")
	enumValue := At(Scope(enumSource), enumRow.Value)
	if _, err := CompileRead(ReadScalar(SelectScalar(enumSource, expr.SumRow(enumValue).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("numeric enum SUM accepted", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(enumSource, expr.MinRow(enumValue).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("numeric enum MIN accepted", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(enumSource, expr.CountRow(enumValue).Value()))); err != nil {
		t.Fatal("enum COUNT rejected", err)
	}
}

func TestNullableDerivedAggregateMetadataCannotBeLaundered(t *testing.T) {
	a, r := derivedAggregateSource()
	other, foreign := derivedAggregateSource()
	invalid := expr.SumRow(At(Scope(a), foreign.Exact))
	for _, v := range []expr.Selected[Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{
		invalid.Value(),
		expr.SumRow(At(Scope(a), r.Exact)).Filter(At(Scope(other), foreign.Exact).Eq(1)).Value(),
	} {
		if _, err := CompileRead(ReadScalar(SelectScalar(a, v))); !errors.Is(err, fault.Invalid) {
			t.Fatal("foreign aggregate metadata accepted", err)
		}
	}
	enumRow := Define(func(b Builder[cell[value.Nullable[numericRowEnum]]], v *cell[value.Nullable[numericRowEnum]]) struct {
		Row[cell[value.Nullable[numericRowEnum]]]
		Value Field[cell[value.Nullable[numericRowEnum]], value.Nullable[numericRowEnum]]
	} {
		return struct {
			Row[cell[value.Nullable[numericRowEnum]]]
			Value Field[cell[value.Nullable[numericRowEnum]], value.Nullable[numericRowEnum]]
		}{b.Row(), NullableEnumOf(b, &v.Value)}
	})
	source := AsRecord[left](RecordOf(Select(a, enumRow, MapSelected(enumRow.Value, expr.RawWithCodec[Alias[left, aggregateRecord]](codec.Nullable(codec.Signed[numericRowEnum]()), "1")))), "nullable_enum")
	input := At(Scope(source), enumRow.Value)
	if _, err := CompileRead(ReadScalar(SelectScalar(source, expr.SumNullableRow(input).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable enum SUM accepted", err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(source, expr.MinNullableRow(input).Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable enum MIN accepted", err)
	}
}
