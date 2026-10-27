package goway_test

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models/scalars"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"testing"
	"time"
)

func scalarGolden[V any](t *testing.T, nv expr.Selected[scalars.Record, V], c codec.Codec[V], sample driver.Value) {
	t.Helper()
	after := &goldenCapture{}
	for _, g := range []*goldenCapture{after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	newDB := after.open(ctx)

	defer newDB.Close(ctx)

	f := rowset.Define(func(b rowset.Builder[windowGoldenRow[V]], r *windowGoldenRow[V]) windowGoldenFields[V] {
		return windowGoldenFields[V]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Read(rowset.Select(rowset.FromModel(scalars.Records.Query()), f, rowset.MapSelected(f.Value, nv))).All(ctx, newDB))
	values := make([]V, len(got))
	for i, r := range got {
		values[i] = r.Value
	}

	fmt.Fprintln(&after.output, oracleSnapshot([]any{values}))
	if after.statements != 1 || !oracleEqual(t, "scalar_golden/1", after.output.String()) {
		t.Fatal("frozen legacy comparison differs")
	}
}
func TestStage6d2ScalarGolden(t *testing.T) {
	nName := expr.Value(scalars.Records.Name)

	nMaybeName := expr.Value(scalars.Records.MaybeName)

	nDate := expr.Value(scalars.Records.Date)

	nMaybeDate := expr.Value(scalars.Records.MaybeDate)

	nLocal := expr.Value(scalars.Records.Local)

	nMaybeLocal := expr.Value(scalars.Records.MaybeLocal)

	nInstant := expr.Value(scalars.Records.Instant)

	nMaybeInstant := expr.Value(scalars.Records.MaybeInstant)

	nClock := expr.Value(scalars.Records.Clock)

	nMaybeClock := expr.Value(scalars.Records.MaybeClock)

	nPeriod := expr.Value(scalars.Records.Period)

	nMaybePeriod := expr.Value(scalars.Records.MaybePeriod)

	nMillis := expr.Value(scalars.Records.Millis)

	nMaybeMillis := expr.Value(scalars.Records.MaybeMillis)

	nLatitude := expr.Value(scalars.Records.Latitude)

	nMaybeLatitude := expr.Value(scalars.Records.MaybeLatitude)
	t.Run("Lower", func(t *testing.T) {
		scalarGolden(t, expr.Lower(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("LowerValue", func(t *testing.T) {
		scalarGolden(t, expr.LowerValue(expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("LowerNullable", func(t *testing.T) {
		scalarGolden(t, expr.LowerNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.LowerNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("LowerNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LowerNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.LowerNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Upper", func(t *testing.T) {
		scalarGolden(t, expr.Upper(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("UpperValue", func(t *testing.T) {
		scalarGolden(t, expr.UpperValue(expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("UpperNullable", func(t *testing.T) {
		scalarGolden(t, expr.UpperNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.UpperNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("UpperNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UpperNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.UpperNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Trim", func(t *testing.T) {
		scalarGolden(t, expr.Trim(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimValue(expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("TrimNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeft", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeft(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftValue(expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimLeftNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeftNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimLeftNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRight", func(t *testing.T) {
		scalarGolden(t, expr.TrimRight(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimRightValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightValue(expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("TrimRightNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimRightNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRightNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimRightNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Length", func(t *testing.T) {
		scalarGolden(t, expr.Length(nName).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthValue", func(t *testing.T) {
		scalarGolden(t, expr.LengthValue(expr.Row(nName).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthNullable", func(t *testing.T) {
		scalarGolden(t, expr.LengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.LengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("LengthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LengthNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.LengthNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLength", func(t *testing.T) {
		scalarGolden(t, expr.OctetLength(nName).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthValue", func(t *testing.T) {
		scalarGolden(t, expr.OctetLengthValue(expr.Row(nName).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthNullable", func(t *testing.T) {
		scalarGolden(t, expr.OctetLengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.OctetLengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLengthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.OctetLengthNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.OctetLengthNullableValue(expr.Row(nMaybeName).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Concat", func(t *testing.T) {
		scalarGolden(t, expr.Concat(nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatValue(expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("ConcatNullable", func(t *testing.T) {
		scalarGolden(t, expr.ConcatNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ConcatNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ConcatNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Replace", func(t *testing.T) {
		scalarGolden(t, expr.Replace(nName, nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ReplaceValue", func(t *testing.T) {
		scalarGolden(t, expr.ReplaceValue(expr.Row(nName).Value(), expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("ReplaceNullable", func(t *testing.T) {
		scalarGolden(t, expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ReplaceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ReplaceNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ReplaceNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatWS", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWS("|", nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWSValue("|", expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullable", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWSNullable("|", nMaybeName, nMaybeName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWSNullableValue("|", expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.String[string](), "héllo")
	})
	t.Run("Substring", func(t *testing.T) {
		scalarGolden(t, expr.Substring(nName, int32(2), int32(3)).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringValue(expr.Row(nName).Value(), int32(2), int32(3)), codec.String[string](), "héllo")
	})
	t.Run("SubstringNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubstringNullable(nMaybeName, int32(2), int32(3)).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringNullable(nMaybeName, int32(2), int32(3)).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringNullableValue(expr.Row(nMaybeName).Value(), int32(2), int32(3)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringNullableValue(expr.Row(nMaybeName).Value(), int32(2), int32(3)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFrom", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFrom(nName, int32(2)).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFromValue(expr.Row(nName).Value(), int32(2)), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFromNullable(nMaybeName, int32(2)).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringFromNullable(nMaybeName, int32(2)).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFromNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFromNullableValue(expr.Row(nMaybeName).Value(), int32(2)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringFromNullableValue(expr.Row(nMaybeName).Value(), int32(2)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAt", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAt(nName, nMillis, nMillis).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAtValue(expr.Row(nName).Value(), expr.Row(nMillis).Value(), expr.Row(nMillis).Value()), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAtNullable(nMaybeName, nMillis, nMillis).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringAtNullable(nMaybeName, nMillis, nMillis).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAtNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAtNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMillis).Value(), expr.Row(nMillis).Value()), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringAtNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMillis).Value(), expr.Row(nMillis).Value()), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Year", func(t *testing.T) {
		scalarGolden(t, expr.Year(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("YearValue", func(t *testing.T) {
		scalarGolden(t, expr.YearValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("YearNullable", func(t *testing.T) {
		scalarGolden(t, expr.YearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.YearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("YearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.YearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.YearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Month", func(t *testing.T) {
		scalarGolden(t, expr.Month(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthValue", func(t *testing.T) {
		scalarGolden(t, expr.MonthValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthNullable", func(t *testing.T) {
		scalarGolden(t, expr.MonthNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MonthNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MonthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MonthNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MonthNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Day", func(t *testing.T) {
		scalarGolden(t, expr.Day(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayValue", func(t *testing.T) {
		scalarGolden(t, expr.DayValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("DayNullable", func(t *testing.T) {
		scalarGolden(t, expr.DayNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeek", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeekValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeek", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeekValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISODayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISODayOfWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYear", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYear(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYearValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearNullable", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfYearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeek", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeekValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOWeekNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYear", func(t *testing.T) {
		scalarGolden(t, expr.ISOYear(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOYearValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearNullable", func(t *testing.T) {
		scalarGolden(t, expr.ISOYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOYearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOYearNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Quarter", func(t *testing.T) {
		scalarGolden(t, expr.Quarter(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterValue", func(t *testing.T) {
		scalarGolden(t, expr.QuarterValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterNullable", func(t *testing.T) {
		scalarGolden(t, expr.QuarterNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.QuarterNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("QuarterNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.QuarterNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.QuarterNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Century", func(t *testing.T) {
		scalarGolden(t, expr.Century(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyValue", func(t *testing.T) {
		scalarGolden(t, expr.CenturyValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyNullable", func(t *testing.T) {
		scalarGolden(t, expr.CenturyNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.CenturyNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("CenturyNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.CenturyNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.CenturyNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Decade", func(t *testing.T) {
		scalarGolden(t, expr.Decade(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeValue", func(t *testing.T) {
		scalarGolden(t, expr.DecadeValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeNullable", func(t *testing.T) {
		scalarGolden(t, expr.DecadeNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DecadeNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DecadeNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DecadeNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DecadeNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Millennium", func(t *testing.T) {
		scalarGolden(t, expr.Millennium(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumValue", func(t *testing.T) {
		scalarGolden(t, expr.MillenniumValue(expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumNullable", func(t *testing.T) {
		scalarGolden(t, expr.MillenniumNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MillenniumNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MillenniumNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MillenniumNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MillenniumNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("JulianDay", func(t *testing.T) {
		scalarGolden(t, expr.JulianDay(nDate).Value(), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayValue", func(t *testing.T) {
		scalarGolden(t, expr.JulianDayValue(expr.Row(nDate).Value()), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayNullable", func(t *testing.T) {
		scalarGolden(t, expr.JulianDayNullable(nMaybeDate).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.JulianDayNullable(nMaybeDate).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("JulianDayNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.JulianDayNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.JulianDayNullableValue(expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Hour", func(t *testing.T) {
		scalarGolden(t, expr.Hour(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("HourValue", func(t *testing.T) {
		scalarGolden(t, expr.HourValue(expr.Row(nClock).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("HourNullable", func(t *testing.T) {
		scalarGolden(t, expr.HourNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.HourNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("HourNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.HourNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.HourNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Minute", func(t *testing.T) {
		scalarGolden(t, expr.Minute(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteValue", func(t *testing.T) {
		scalarGolden(t, expr.MinuteValue(expr.Row(nClock).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteNullable", func(t *testing.T) {
		scalarGolden(t, expr.MinuteNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MinuteNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MinuteNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MinuteNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MinuteNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Microseconds", func(t *testing.T) {
		scalarGolden(t, expr.Microseconds(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.MicrosecondsValue(expr.Row(nClock).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.MicrosecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MicrosecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MicrosecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MicrosecondsNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MicrosecondsNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Second", func(t *testing.T) {
		scalarGolden(t, expr.Second(nClock).Value(), codec.Decimal(), "12.34")
	})
	t.Run("SecondValue", func(t *testing.T) {
		scalarGolden(t, expr.SecondValue(expr.Row(nClock).Value()), codec.Decimal(), "12.34")
	})
	t.Run("SecondNullable", func(t *testing.T) {
		scalarGolden(t, expr.SecondNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.SecondNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("SecondNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SecondNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.SecondNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Milliseconds", func(t *testing.T) {
		scalarGolden(t, expr.Milliseconds(nClock).Value(), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.MillisecondsValue(expr.Row(nClock).Value()), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.MillisecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.MillisecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("MillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MillisecondsNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.MillisecondsNullableValue(expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSeconds", func(t *testing.T) {
		scalarGolden(t, expr.UnixSeconds(nInstant).Value(), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixSecondsValue(expr.Row(nInstant).Value()), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.UnixSecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.UnixSecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixSecondsNullableValue(expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.UnixSecondsNullableValue(expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixMilliseconds", func(t *testing.T) {
		scalarGolden(t, expr.UnixMilliseconds(nInstant).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixMillisecondsValue(expr.Row(nInstant).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.UnixMillisecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.UnixMillisecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("UnixMillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixMillisecondsNullableValue(expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.UnixMillisecondsNullableValue(expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("TruncateDate", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDate(nDate, expr.DateMonth).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDateValue(expr.Row(nDate).Value(), expr.DateMonth), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateNullable", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDateNullable(nMaybeDate, expr.DateMonth).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.TruncateDateNullable(nMaybeDate, expr.DateMonth).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateDateNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDateNullableValue(expr.Row(nMaybeDate).Value(), expr.DateMonth), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.TruncateDateNullableValue(expr.Row(nMaybeDate).Value(), expr.DateMonth), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateLocal", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocal(nLocal, expr.TimestampHour).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocalValue(expr.Row(nLocal).Value(), expr.TimestampHour), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.TruncateLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateInstant", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstant(nInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstantValue(expr.Row(nInstant).Value(), expr.TimestampDay, expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantNullable", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("TruncateInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstantNullableValue(expr.Row(nMaybeInstant).Value(), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.TruncateInstantNullableValue(expr.Row(nMaybeInstant).Value(), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillis", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillis(nMillis).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisValue", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillisValue(expr.Row(nMillis).Value()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisNullable", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillisNullable(nMaybeMillis).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.FromUnixMillisNullable(nMaybeMillis).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillisNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillisNullableValue(expr.Row(nMaybeMillis).Value()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.FromUnixMillisNullableValue(expr.Row(nMaybeMillis).Value()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("LocalAt", func(t *testing.T) {
		scalarGolden(t, expr.LocalAt(nInstant, expr.UTCZone()).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalAtValue(expr.Row(nInstant).Value(), expr.UTCZone()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtNullable", func(t *testing.T) {
		scalarGolden(t, expr.LocalAtNullable(nMaybeInstant, expr.UTCZone()).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.LocalAtNullable(nMaybeInstant, expr.UTCZone()).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("LocalAtNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalAtNullableValue(expr.Row(nMaybeInstant).Value(), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.LocalAtNullableValue(expr.Row(nMaybeInstant).Value(), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ResolveLocal", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocal(nLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocalValue(expr.Row(nLocal).Value(), expr.UTCZone(), expr.PostgresStandardTime), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ResolveLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ResolveLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("DateOf", func(t *testing.T) {
		scalarGolden(t, expr.DateOf(nLocal).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfValue", func(t *testing.T) {
		scalarGolden(t, expr.DateOfValue(expr.Row(nLocal).Value()), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfNullable", func(t *testing.T) {
		scalarGolden(t, expr.DateOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.DateOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateOfNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DateOfNullableValue(expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.DateOfNullableValue(expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TimeOf", func(t *testing.T) {
		scalarGolden(t, expr.TimeOf(nLocal).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfValue", func(t *testing.T) {
		scalarGolden(t, expr.TimeOfValue(expr.Row(nLocal).Value()), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfNullable", func(t *testing.T) {
		scalarGolden(t, expr.TimeOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.TimeOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("TimeOfNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TimeOfNullableValue(expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.TimeOfNullableValue(expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("CombineDateTime", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTime(nDate, nClock).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeValue", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTimeValue(expr.Row(nDate).Value(), expr.Row(nClock).Value()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeNullable", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("CombineDateTimeNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTimeNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybeClock).Value()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.CombineDateTimeNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybeClock).Value()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateDays", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDays(nDate, int32(2)).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDaysValue(expr.Row(nDate).Value(), int32(2)), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDaysNullable(nMaybeDate, int32(2)).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AddDateDaysNullable(nMaybeDate, int32(2)).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("AddDateDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDaysNullableValue(expr.Row(nMaybeDate).Value(), int32(2)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AddDateDaysNullableValue(expr.Row(nMaybeDate).Value(), int32(2)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateDifference", func(t *testing.T) {
		scalarGolden(t, expr.DateDifference(nDate, nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.DateDifferenceValue(expr.Row(nDate).Value(), expr.Row(nDate).Value()), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.DateDifferenceNullable(nMaybeDate, nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DateDifferenceNullable(nMaybeDate, nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DateDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DateDifferenceNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DateDifferenceNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybeDate).Value()), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("AddDateInterval", func(t *testing.T) {
		scalarGolden(t, expr.AddDateInterval(nDate, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateIntervalValue(expr.Row(nDate).Value(), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateIntervalNullableValue(expr.Row(nMaybeDate).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddDateIntervalNullableValue(expr.Row(nMaybeDate).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalInterval", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalInterval(nLocal, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalIntervalValue(expr.Row(nLocal).Value(), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalIntervalNullableValue(expr.Row(nMaybeLocal).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddLocalIntervalNullableValue(expr.Row(nMaybeLocal).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddInstantInterval", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantInterval(nInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantIntervalValue(expr.Row(nInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantIntervalNullableValue(expr.Row(nMaybeInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AddInstantIntervalNullableValue(expr.Row(nMaybeInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddClockElapsed", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsed(nClock, time.Second).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsedValue(expr.Row(nClock).Value(), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AddClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsedNullableValue(expr.Row(nMaybeClock).Value(), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AddClockElapsedNullableValue(expr.Row(nMaybeClock).Value(), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractDateInterval", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateInterval(nDate, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateIntervalValue(expr.Row(nDate).Value(), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateIntervalNullableValue(expr.Row(nMaybeDate).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractDateIntervalNullableValue(expr.Row(nMaybeDate).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalInterval", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalInterval(nLocal, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalIntervalValue(expr.Row(nLocal).Value(), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalIntervalNullableValue(expr.Row(nMaybeLocal).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractLocalIntervalNullableValue(expr.Row(nMaybeLocal).Value(), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractInstantInterval", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantInterval(nInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantIntervalValue(expr.Row(nInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantIntervalNullableValue(expr.Row(nMaybeInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.SubtractInstantIntervalNullableValue(expr.Row(nMaybeInstant).Value(), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractClockElapsed", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsed(nClock, time.Second).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsedValue(expr.Row(nClock).Value(), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.SubtractClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsedNullableValue(expr.Row(nMaybeClock).Value(), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.SubtractClockElapsedNullableValue(expr.Row(nMaybeClock).Value(), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddIntervals", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervals(nPeriod, nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsValue", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervalsValue(expr.Row(nPeriod).Value(), expr.Row(nPeriod).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("AddIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervalsNullableValue(expr.Row(nMaybePeriod).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AddIntervalsNullableValue(expr.Row(nMaybePeriod).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervals", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervals(nPeriod, nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervalsValue(expr.Row(nPeriod).Value(), expr.Row(nPeriod).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervalsNullableValue(expr.Row(nMaybePeriod).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.SubtractIntervalsNullableValue(expr.Row(nMaybePeriod).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateInterval", func(t *testing.T) {
		scalarGolden(t, expr.NegateInterval(nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.NegateIntervalValue(expr.Row(nPeriod).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.NegateIntervalNullable(nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.NegateIntervalNullable(nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.NegateIntervalNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.NegateIntervalNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifference", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifference(nLocal, nLocal).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifferenceValue(expr.Row(nLocal).Value(), expr.Row(nLocal).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifferenceNullableValue(expr.Row(nMaybeLocal).Value(), expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.LocalDifferenceNullableValue(expr.Row(nMaybeLocal).Value(), expr.Row(nMaybeLocal).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifference", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifference(nInstant, nInstant).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifferenceValue(expr.Row(nInstant).Value(), expr.Row(nInstant).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifferenceNullableValue(expr.Row(nMaybeInstant).Value(), expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.InstantDifferenceNullableValue(expr.Row(nMaybeInstant).Value(), expr.Row(nMaybeInstant).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifference", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifference(nClock, nClock).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifferenceValue(expr.Row(nClock).Value(), expr.Row(nClock).Value()), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifferenceNullableValue(expr.Row(nMaybeClock).Value(), expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.ClockDifferenceNullableValue(expr.Row(nMaybeClock).Value(), expr.Row(nMaybeClock).Value()), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("IntervalMonths", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonths(nPeriod).Value(), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonthsValue(expr.Row(nPeriod).Value()), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsNullable", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonthsNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalMonthsNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalMonthsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonthsNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalMonthsNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDays", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDays(nPeriod).Value(), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDaysValue(expr.Row(nPeriod).Value()), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysNullable", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDaysNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalDaysNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDaysNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalDaysNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalElapsed", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsed(nPeriod).Value(), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsedValue(expr.Row(nPeriod).Value()), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsedNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, expr.IntervalElapsedNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("IntervalElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsedNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, expr.IntervalElapsedNullableValue(expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("ShiftDate", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDate(nDate, nPeriod).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDateValue(expr.Row(nDate).Value(), expr.Row(nPeriod).Value()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateNullable", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDateNullable(nMaybeDate, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftDateNullable(nMaybeDate, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftDateNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDateNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftDateNullableValue(expr.Row(nMaybeDate).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocal", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocal(nLocal, nPeriod).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocalValue(expr.Row(nLocal).Value(), expr.Row(nPeriod).Value()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftLocalNullableValue(expr.Row(nMaybeLocal).Value(), expr.Row(nMaybePeriod).Value()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftInstant", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstant(nInstant, nPeriod, expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstantValue(expr.Row(nInstant).Value(), expr.Row(nPeriod).Value(), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantNullable", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ShiftInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstantNullableValue(expr.Row(nMaybeInstant).Value(), expr.Row(nMaybePeriod).Value(), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ShiftInstantNullableValue(expr.Row(nMaybeInstant).Value(), expr.Row(nMaybePeriod).Value(), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SphericalDistance", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistance(nLatitude, nLatitude, expr.GeographicPoint{}).Value(), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceValue", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistanceValue(expr.Row(nLatitude).Value(), expr.Row(nLatitude).Value(), expr.GeographicPoint{}), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceNullable", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{}).Value(), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{}).Value(), codec.Nullable(codec.Float[float64]()), nil)
	})
	t.Run("SphericalDistanceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistanceNullableValue(expr.Row(nMaybeLatitude).Value(), expr.Row(nMaybeLatitude).Value(), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, expr.SphericalDistanceNullableValue(expr.Row(nMaybeLatitude).Value(), expr.Row(nMaybeLatitude).Value(), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), nil)
	})
	scalarGolden(t, expr.TransactionTime(rowset.ModelScope(scalars.Records.Query())).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	t.Run("TrimChars", func(t *testing.T) {
		scalarGolden(t, expr.TrimChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimCharsValue(expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "abc")
	})
	t.Run("TrimCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimCharsNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftChars", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftCharsValue(expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftCharsNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightChars", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightCharsValue(expr.Row(nName).Value(), expr.Row(nName).Value()), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightCharsNullableValue(expr.Row(nMaybeName).Value(), expr.Row(nMaybeName).Value()), codec.Nullable(codec.String[string]()), "abc")
	})
}
