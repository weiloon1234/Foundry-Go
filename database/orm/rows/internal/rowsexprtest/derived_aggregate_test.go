package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	rows.Row[aggregateRecord]
	ID          rows.Field[aggregateRecord, int64]
	Exact       rows.Field[aggregateRecord, int64]
	MaybeExact  rows.Field[aggregateRecord, value.Nullable[int64]]
	Approx      rows.Field[aggregateRecord, float64]
	MaybeApprox rows.Field[aggregateRecord, value.Nullable[float64]]
	Period      rows.Field[aggregateRecord, temporal.Interval]
	MaybePeriod rows.Field[aggregateRecord, value.Nullable[temporal.Interval]]
	Name        rows.Field[aggregateRecord, aggregateName]
	MaybeName   rows.Field[aggregateRecord, value.Nullable[aggregateName]]
}

func derivedAggregateSource() (rows.Aliased[left, aggregateRecord], aggregateRowFields) {
	d := declareAggregateRecords()
	r := rows.Define(func(b rows.Builder[aggregateRecord], v *aggregateRecord) aggregateRowFields {
		return aggregateRowFields{Row: b.Row(),
			ID:          rows.NumberOf(b, &v.ID),
			Exact:       rows.NumberOf(b, &v.Exact),
			MaybeExact:  rows.NullableNumberOf(b, &v.MaybeExact),
			Approx:      rows.NumberOf(b, &v.Approx),
			MaybeApprox: rows.NullableNumberOf(b, &v.MaybeApprox),
			Period:      rows.FieldOf(b, &v.Period, orm.Codec(codec.Interval())),
			MaybePeriod: rows.NullableFieldOf(b, &v.MaybePeriod, orm.Codec(codec.Interval())),
			Name:        rows.TextOf(b, &v.Name),
			MaybeName:   rows.NullableTextOf(b, &v.MaybeName),
		}
	})
	q := rows.Select(rows.From(d.Query()), r.Row,
		rows.Map(r.ID, expr.Value(orm.KeyExpr(d.Key()))),
		rows.Map(r.Exact, expr.Value(orm.NumberExpr(d.Exact))),
		rows.Map(r.MaybeExact, expr.Value(orm.NullableNumberExpr(d.MaybeExact))),
		rows.Map(r.Approx, expr.Value(orm.NumberExpr(d.Approx))),
		rows.Map(r.MaybeApprox, expr.Value(orm.NullableNumberExpr(d.MaybeApprox))),
		rows.Map(r.Period, expr.Value(orm.IntervalExpr(d.Period))),
		rows.Map(r.MaybePeriod, expr.Value(orm.NullableIntervalExpr(d.MaybePeriod))),
		rows.Map(r.Name, expr.Value(orm.TextExpr(d.Name))),
		rows.Map(r.MaybeName, expr.Value(orm.NullableTextExpr(d.MaybeName))),
	)
	return rows.AsRecord[left](rows.RecordOf(q), "derived"), r
}

func TestDerivedAggregateFamilies(t *testing.T) {
	a, r := derivedAggregateSource()
	s := rows.Scope(a)
	t.Run("SumRow", func(t *testing.T) {
		aggregate := expr.SumRow(rows.At(s, r.Exact))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumNullableRow", func(t *testing.T) {
		aggregate := expr.SumNullableRow(rows.At(s, r.MaybeExact))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgRow", func(t *testing.T) {
		aggregate := expr.AvgRow(rows.At(s, r.Exact))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgNullableRow", func(t *testing.T) {
		aggregate := expr.AvgNullableRow(rows.At(s, r.MaybeExact))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumFloatRow", func(t *testing.T) {
		aggregate := expr.SumFloatRow(rows.At(s, r.Approx))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumFloatNullableRow", func(t *testing.T) {
		aggregate := expr.SumFloatNullableRow(rows.At(s, r.MaybeApprox))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgFloatRow", func(t *testing.T) {
		aggregate := expr.AvgFloatRow(rows.At(s, r.Approx))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgFloatNullableRow", func(t *testing.T) {
		aggregate := expr.AvgFloatNullableRow(rows.At(s, r.MaybeApprox))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumIntervalRow", func(t *testing.T) {
		aggregate := expr.SumIntervalRow(rows.At(s, r.Period))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("SumIntervalNullableRow", func(t *testing.T) {
		aggregate := expr.SumIntervalNullableRow(rows.At(s, r.MaybePeriod))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgIntervalRow", func(t *testing.T) {
		aggregate := expr.AvgIntervalRow(rows.At(s, r.Period))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("AvgIntervalNullableRow", func(t *testing.T) {
		aggregate := expr.AvgIntervalNullableRow(rows.At(s, r.MaybePeriod))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MinRow", func(t *testing.T) {
		aggregate := expr.MinRow(rows.At(s, r.Name))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MinNullableRow", func(t *testing.T) {
		aggregate := expr.MinNullableRow(rows.At(s, r.MaybeName))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MaxRow", func(t *testing.T) {
		aggregate := expr.MaxRow(rows.At(s, r.Name))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	t.Run("MaxNullableRow", func(t *testing.T) {
		aggregate := expr.MaxNullableRow(rows.At(s, r.MaybeName))
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
		_, column := expr.FoundryAggregate(aggregate, sqlowner.Expressions())
		if column.Scan == nil {
			t.Fatal("missing aggregate codec")
		}
	})
	for _, aggregate := range []expr.OrderedAggregate[rows.Alias[left, aggregateRecord], int64]{expr.CountRow(rows.At(s, r.Exact)), expr.CountDistinctRow(rows.At(s, r.MaybeExact))} {
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(aggregate)))); err != nil {
			t.Fatal(err)
		}
	}
	// Nullable min/max retain the codec and accept present fallback values.
	minimum := expr.MinNullableRow(rows.At(s, r.MaybeName))
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
	fallback := expr.CoalesceValue(expr.AsSelected(minimum), expr.AsSelected(expr.ParamOf(minimum, aggregateName("fallback"))))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, fallback))); err != nil {
		t.Fatal(err)
	}
	if got, err := expr.FoundrySelectedColumn(expr.AsSelected(expr.ParamOf(minimum, aggregateName("fallback"))), sqlowner.Expressions()).Bind(aggregateName("fallback")); err != nil || got != "fallback" {
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
	foreignSum := expr.SumRow(rows.At(rows.Scope(other), foreign.Exact))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(foreignSum)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign aggregate accepted", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(expr.SumRow(expr.ParamOf(rows.At(rows.Scope(a), r.Exact), 1)))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("parameter accepted as a stored field", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, expr.AsSelected(expr.SumRow(expr.RowExpression[rows.Alias[left, aggregateRecord], int64]{}))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero aggregate accepted", err)
	}
	enumRow := rows.Define(func(b rows.Builder[cell[numericRowEnum]], v *cell[numericRowEnum]) struct {
		rows.Row[cell[numericRowEnum]]
		Value rows.Field[cell[numericRowEnum], numericRowEnum]
	} {
		return struct {
			rows.Row[cell[numericRowEnum]]
			Value rows.Field[cell[numericRowEnum], numericRowEnum]
		}{b.Row(), rows.EnumOf(b, &v.Value)}
	})
	// Feed the concrete declaration metadata through the same sealed rows.At path.
	enumSource := rows.AsRecord[left](rows.RecordOf(rows.Select(a, enumRow, rows.MapSelected(enumRow.Value, expr.Raw[rows.Alias[left, aggregateRecord], numericRowEnum]("1")))), "enum_input")
	enumValue := rows.At(rows.Scope(enumSource), enumRow.Value)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(enumSource, expr.AsSelected(expr.SumRow(enumValue))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("numeric enum SUM accepted", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(enumSource, expr.AsSelected(expr.MinRow(enumValue))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("numeric enum MIN accepted", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(enumSource, expr.AsSelected(expr.CountRow(enumValue))))); err != nil {
		t.Fatal("enum COUNT rejected", err)
	}
}

func TestNullableDerivedAggregateMetadataCannotBeLaundered(t *testing.T) {
	a, r := derivedAggregateSource()
	other, foreign := derivedAggregateSource()
	invalid := expr.SumRow(rows.At(rows.Scope(a), foreign.Exact))
	for _, v := range []expr.Selected[rows.Alias[left, aggregateRecord], value.Nullable[decimal.Decimal]]{
		expr.AsSelected(invalid),
		expr.AsSelected(expr.Filter(expr.SumRow(rows.At(rows.Scope(a), r.Exact)), expr.Eq(rows.At(rows.Scope(other), foreign.Exact), 1))),
	} {
		if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(a, v))); !errors.Is(err, fault.Invalid) {
			t.Fatal("foreign aggregate metadata accepted", err)
		}
	}
	enumRow := rows.Define(func(b rows.Builder[cell[value.Nullable[numericRowEnum]]], v *cell[value.Nullable[numericRowEnum]]) struct {
		rows.Row[cell[value.Nullable[numericRowEnum]]]
		Value rows.Field[cell[value.Nullable[numericRowEnum]], value.Nullable[numericRowEnum]]
	} {
		return struct {
			rows.Row[cell[value.Nullable[numericRowEnum]]]
			Value rows.Field[cell[value.Nullable[numericRowEnum]], value.Nullable[numericRowEnum]]
		}{b.Row(), rows.NullableEnumOf(b, &v.Value)}
	})
	source := rows.AsRecord[left](rows.RecordOf(rows.Select(a, enumRow, rows.MapSelected(enumRow.Value, expr.RawWithCodec[rows.Alias[left, aggregateRecord]](codec.Nullable(codec.Signed[numericRowEnum]()), "1")))), "nullable_enum")
	input := rows.At(rows.Scope(source), enumRow.Value)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.SumNullableRow(input))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable enum SUM accepted", err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(source, expr.AsSelected(expr.MinNullableRow(input))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable enum MIN accepted", err)
	}
}
