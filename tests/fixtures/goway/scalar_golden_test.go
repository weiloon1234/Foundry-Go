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
	nName := expr.Value(orm.TextExpr(scalars.Records.Name))

	nMaybeName := expr.Value(orm.NullableTextExpr(scalars.Records.MaybeName))

	nDate := expr.Value(orm.TimeExpr(scalars.Records.Date))

	nMaybeDate := expr.Value(orm.NullableTimeExpr(scalars.Records.MaybeDate))

	nLocal := expr.Value(orm.TimeExpr(scalars.Records.Local))

	nMaybeLocal := expr.Value(orm.NullableTimeExpr(scalars.Records.MaybeLocal))

	nInstant := expr.Value(orm.TimeExpr(scalars.Records.Instant))

	nMaybeInstant := expr.Value(orm.NullableTimeExpr(scalars.Records.MaybeInstant))

	nClock := expr.Value(orm.TimeExpr(scalars.Records.Clock))

	nMaybeClock := expr.Value(orm.NullableTimeExpr(scalars.Records.MaybeClock))

	nPeriod := expr.Value(orm.IntervalExpr(scalars.Records.Period))

	nMaybePeriod := expr.Value(orm.NullableIntervalExpr(scalars.Records.MaybePeriod))

	nMillis := expr.Value(orm.NumberExpr(scalars.Records.Millis))

	nMaybeMillis := expr.Value(orm.NullableNumberExpr(scalars.Records.MaybeMillis))

	nLatitude := expr.Value(orm.NumberExpr(scalars.Records.Latitude))

	nMaybeLatitude := expr.Value(orm.NullableNumberExpr(scalars.Records.MaybeLatitude))
	t.Run("Lower", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Lower(nName)), codec.String[string](), "héllo")
	})
	t.Run("LowerValue", func(t *testing.T) {
		scalarGolden(t, expr.LowerValue(expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("LowerNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LowerNullable(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.LowerNullable(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("LowerNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LowerNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.LowerNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Upper", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Upper(nName)), codec.String[string](), "héllo")
	})
	t.Run("UpperValue", func(t *testing.T) {
		scalarGolden(t, expr.UpperValue(expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("UpperNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.UpperNullable(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.UpperNullable(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("UpperNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UpperNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.UpperNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Trim", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Trim(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimValue(expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("TrimNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimNullable(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.TrimNullable(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeft", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimLeft(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftValue(expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimLeftNullable(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.TrimLeftNullable(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeftNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimLeftNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRight", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimRight(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimRightValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightValue(expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("TrimRightNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimRightNullable(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.TrimRightNullable(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRightNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.TrimRightNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Length", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Length(nName)), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthValue", func(t *testing.T) {
		scalarGolden(t, expr.LengthValue(expr.AsSelected(expr.Row(nName))), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LengthNullable(nMaybeName)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.LengthNullable(nMaybeName)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("LengthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LengthNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.LengthNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLength", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.OctetLength(nName)), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthValue", func(t *testing.T) {
		scalarGolden(t, expr.OctetLengthValue(expr.AsSelected(expr.Row(nName))), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.OctetLengthNullable(nMaybeName)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.OctetLengthNullable(nMaybeName)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLengthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.OctetLengthNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.OctetLengthNullableValue(expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Concat", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Concat(nName, nName)), codec.String[string](), "héllo")
	})
	t.Run("ConcatValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("ConcatNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ConcatNullable(nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.ConcatNullable(nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ConcatNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Replace", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Replace(nName, nName, nName)), codec.String[string](), "héllo")
	})
	t.Run("ReplaceValue", func(t *testing.T) {
		scalarGolden(t, expr.ReplaceValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("ReplaceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ReplaceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ReplaceNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.ReplaceNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatWS", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ConcatWS("|", nName, nName)), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWSValue("|", expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ConcatWSNullable("|", nMaybeName, nMaybeName)), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ConcatWSNullableValue("|", expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.String[string](), "héllo")
	})
	t.Run("Substring", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Substring(nName, int32(2), int32(3))), codec.String[string](), "héllo")
	})
	t.Run("SubstringValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringValue(expr.AsSelected(expr.Row(nName)), int32(2), int32(3)), codec.String[string](), "héllo")
	})
	t.Run("SubstringNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubstringNullable(nMaybeName, int32(2), int32(3))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.SubstringNullable(nMaybeName, int32(2), int32(3))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringNullableValue(expr.AsSelected(expr.Row(nMaybeName)), int32(2), int32(3)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringNullableValue(expr.AsSelected(expr.Row(nMaybeName)), int32(2), int32(3)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFrom", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubstringFrom(nName, int32(2))), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFromValue(expr.AsSelected(expr.Row(nName)), int32(2)), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubstringFromNullable(nMaybeName, int32(2))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.SubstringFromNullable(nMaybeName, int32(2))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFromNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringFromNullableValue(expr.AsSelected(expr.Row(nMaybeName)), int32(2)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringFromNullableValue(expr.AsSelected(expr.Row(nMaybeName)), int32(2)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAt", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubstringAt(nName, nMillis, nMillis)), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAtValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nMillis)), expr.AsSelected(expr.Row(nMillis))), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubstringAtNullable(nMaybeName, nMillis, nMillis)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.AsSelected(expr.SubstringAtNullable(nMaybeName, nMillis, nMillis)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAtNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubstringAtNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMillis)), expr.AsSelected(expr.Row(nMillis))), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, expr.SubstringAtNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMillis)), expr.AsSelected(expr.Row(nMillis))), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Year", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Year(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("YearValue", func(t *testing.T) {
		scalarGolden(t, expr.YearValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("YearNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.YearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.YearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("YearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.YearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.YearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Month", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Month(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthValue", func(t *testing.T) {
		scalarGolden(t, expr.MonthValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.MonthNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.MonthNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MonthNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MonthNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MonthNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Day", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Day(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayValue", func(t *testing.T) {
		scalarGolden(t, expr.DayValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("DayNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DayNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.DayNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeek", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DayOfWeek(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeekValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DayOfWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.DayOfWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeek", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISODayOfWeek(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeekValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISODayOfWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.ISODayOfWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISODayOfWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISODayOfWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYear", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DayOfYear(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYearValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DayOfYearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.DayOfYearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DayOfYearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DayOfYearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeek", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISOWeek(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeekValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISOWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.ISOWeekNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOWeekNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYear", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISOYear(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOYearValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ISOYearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.ISOYearNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYearNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ISOYearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.ISOYearNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Quarter", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Quarter(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterValue", func(t *testing.T) {
		scalarGolden(t, expr.QuarterValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.QuarterNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.QuarterNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("QuarterNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.QuarterNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.QuarterNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Century", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Century(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyValue", func(t *testing.T) {
		scalarGolden(t, expr.CenturyValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.CenturyNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.CenturyNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("CenturyNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.CenturyNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.CenturyNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Decade", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Decade(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeValue", func(t *testing.T) {
		scalarGolden(t, expr.DecadeValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DecadeNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.DecadeNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DecadeNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DecadeNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DecadeNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Millennium", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Millennium(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumValue", func(t *testing.T) {
		scalarGolden(t, expr.MillenniumValue(expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.MillenniumNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.MillenniumNullable(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MillenniumNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MillenniumNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MillenniumNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("JulianDay", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.JulianDay(nDate)), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayValue", func(t *testing.T) {
		scalarGolden(t, expr.JulianDayValue(expr.AsSelected(expr.Row(nDate))), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.JulianDayNullable(nMaybeDate)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.AsSelected(expr.JulianDayNullable(nMaybeDate)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("JulianDayNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.JulianDayNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.JulianDayNullableValue(expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Hour", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Hour(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("HourValue", func(t *testing.T) {
		scalarGolden(t, expr.HourValue(expr.AsSelected(expr.Row(nClock))), codec.Signed[int64](), int64(3))
	})
	t.Run("HourNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.HourNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.HourNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("HourNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.HourNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.HourNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Minute", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Minute(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteValue", func(t *testing.T) {
		scalarGolden(t, expr.MinuteValue(expr.AsSelected(expr.Row(nClock))), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.MinuteNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.MinuteNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MinuteNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MinuteNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MinuteNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Microseconds", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Microseconds(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.MicrosecondsValue(expr.AsSelected(expr.Row(nClock))), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.MicrosecondsNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.MicrosecondsNullable(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MicrosecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MicrosecondsNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.MicrosecondsNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Second", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Second(nClock)), codec.Decimal(), "12.34")
	})
	t.Run("SecondValue", func(t *testing.T) {
		scalarGolden(t, expr.SecondValue(expr.AsSelected(expr.Row(nClock))), codec.Decimal(), "12.34")
	})
	t.Run("SecondNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SecondNullable(nMaybeClock)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.AsSelected(expr.SecondNullable(nMaybeClock)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("SecondNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SecondNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.SecondNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Milliseconds", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.Milliseconds(nClock)), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.MillisecondsValue(expr.AsSelected(expr.Row(nClock))), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.MillisecondsNullable(nMaybeClock)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.AsSelected(expr.MillisecondsNullable(nMaybeClock)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("MillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.MillisecondsNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.MillisecondsNullableValue(expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSeconds", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.UnixSeconds(nInstant)), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixSecondsValue(expr.AsSelected(expr.Row(nInstant))), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.UnixSecondsNullable(nMaybeInstant)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.AsSelected(expr.UnixSecondsNullable(nMaybeInstant)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixSecondsNullableValue(expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, expr.UnixSecondsNullableValue(expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixMilliseconds", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.UnixMilliseconds(nInstant)), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixMillisecondsValue(expr.AsSelected(expr.Row(nInstant))), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.UnixMillisecondsNullable(nMaybeInstant)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.UnixMillisecondsNullable(nMaybeInstant)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("UnixMillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.UnixMillisecondsNullableValue(expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.UnixMillisecondsNullableValue(expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("TruncateDate", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateDate(nDate, expr.DateMonth)), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDateValue(expr.AsSelected(expr.Row(nDate)), expr.DateMonth), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateDateNullable(nMaybeDate, expr.DateMonth)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AsSelected(expr.TruncateDateNullable(nMaybeDate, expr.DateMonth)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateDateNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateDateNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.DateMonth), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.TruncateDateNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.DateMonth), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateLocal", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateLocal(nLocal, expr.TimestampHour)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocalValue(expr.AsSelected(expr.Row(nLocal)), expr.TimestampHour), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.TruncateLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateInstant", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateInstant(nInstant, expr.TimestampDay, expr.UTCZone())), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstantValue(expr.AsSelected(expr.Row(nInstant)), expr.TimestampDay, expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone())), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone())), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("TruncateInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TruncateInstantNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.TruncateInstantNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillis", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.FromUnixMillis(nMillis)), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisValue", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillisValue(expr.AsSelected(expr.Row(nMillis))), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.FromUnixMillisNullable(nMaybeMillis)), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.FromUnixMillisNullable(nMaybeMillis)), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillisNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.FromUnixMillisNullableValue(expr.AsSelected(expr.Row(nMaybeMillis))), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.FromUnixMillisNullableValue(expr.AsSelected(expr.Row(nMaybeMillis))), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("LocalAt", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LocalAt(nInstant, expr.UTCZone())), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalAtValue(expr.AsSelected(expr.Row(nInstant)), expr.UTCZone()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LocalAtNullable(nMaybeInstant, expr.UTCZone())), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.LocalAtNullable(nMaybeInstant, expr.UTCZone())), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("LocalAtNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalAtNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.LocalAtNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ResolveLocal", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ResolveLocal(nLocal, expr.UTCZone(), expr.PostgresStandardTime)), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocalValue(expr.AsSelected(expr.Row(nLocal)), expr.UTCZone(), expr.PostgresStandardTime), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime)), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime)), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ResolveLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ResolveLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ResolveLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("DateOf", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DateOf(nLocal)), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfValue", func(t *testing.T) {
		scalarGolden(t, expr.DateOfValue(expr.AsSelected(expr.Row(nLocal))), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DateOfNullable(nMaybeLocal)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AsSelected(expr.DateOfNullable(nMaybeLocal)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateOfNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DateOfNullableValue(expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.DateOfNullableValue(expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TimeOf", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TimeOf(nLocal)), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfValue", func(t *testing.T) {
		scalarGolden(t, expr.TimeOfValue(expr.AsSelected(expr.Row(nLocal))), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TimeOfNullable(nMaybeLocal)), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AsSelected(expr.TimeOfNullable(nMaybeLocal)), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("TimeOfNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TimeOfNullableValue(expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.TimeOfNullableValue(expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("CombineDateTime", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.CombineDateTime(nDate, nClock)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeValue", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTimeValue(expr.AsSelected(expr.Row(nDate)), expr.AsSelected(expr.Row(nClock))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("CombineDateTimeNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.CombineDateTimeNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.CombineDateTimeNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateDays", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddDateDays(nDate, int32(2))), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDaysValue(expr.AsSelected(expr.Row(nDate)), int32(2)), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddDateDaysNullable(nMaybeDate, int32(2))), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AsSelected(expr.AddDateDaysNullable(nMaybeDate, int32(2))), codec.Nullable(codec.Date()), nil)
	})
	t.Run("AddDateDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateDaysNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), int32(2)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, expr.AddDateDaysNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), int32(2)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateDifference", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DateDifference(nDate, nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.DateDifferenceValue(expr.AsSelected(expr.Row(nDate)), expr.AsSelected(expr.Row(nDate))), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.DateDifferenceNullable(nMaybeDate, nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.DateDifferenceNullable(nMaybeDate, nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DateDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.DateDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, expr.DateDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybeDate))), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("AddDateInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddDateInterval(nDate, temporal.Days(2))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateIntervalValue(expr.AsSelected(expr.Row(nDate)), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddDateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddDateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddLocalInterval(nLocal, temporal.Days(2))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalIntervalValue(expr.AsSelected(expr.Row(nLocal)), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddLocalIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AddLocalIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddInstantInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddInstantInterval(nInstant, temporal.Days(2), expr.UTCZone())), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantIntervalValue(expr.AsSelected(expr.Row(nInstant)), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone())), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone())), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddInstantIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AddInstantIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddClockElapsed", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddClockElapsed(nClock, time.Second)), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsedValue(expr.AsSelected(expr.Row(nClock)), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddClockElapsedNullable(nMaybeClock, time.Second)), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AsSelected(expr.AddClockElapsedNullable(nMaybeClock, time.Second)), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddClockElapsedNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AddClockElapsedNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractDateInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractDateInterval(nDate, temporal.Days(2))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateIntervalValue(expr.AsSelected(expr.Row(nDate)), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractDateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractDateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractLocalInterval(nLocal, temporal.Days(2))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalIntervalValue(expr.AsSelected(expr.Row(nLocal)), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractLocalIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.SubtractLocalIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractInstantInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractInstantInterval(nInstant, temporal.Days(2), expr.UTCZone())), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantIntervalValue(expr.AsSelected(expr.Row(nInstant)), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone())), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone())), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractInstantIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.SubtractInstantIntervalNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractClockElapsed", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractClockElapsed(nClock, time.Second)), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsedValue(expr.AsSelected(expr.Row(nClock)), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractClockElapsedNullable(nMaybeClock, time.Second)), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.AsSelected(expr.SubtractClockElapsedNullable(nMaybeClock, time.Second)), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractClockElapsedNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, expr.SubtractClockElapsedNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddIntervals", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddIntervals(nPeriod, nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsValue", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervalsValue(expr.AsSelected(expr.Row(nPeriod)), expr.AsSelected(expr.Row(nPeriod))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("AddIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.AddIntervalsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AddIntervalsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervals", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractIntervals(nPeriod, nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervalsValue(expr.AsSelected(expr.Row(nPeriod)), expr.AsSelected(expr.Row(nPeriod))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SubtractIntervalsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.SubtractIntervalsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateInterval", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.NegateInterval(nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalValue", func(t *testing.T) {
		scalarGolden(t, expr.NegateIntervalValue(expr.AsSelected(expr.Row(nPeriod))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.NegateIntervalNullable(nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.NegateIntervalNullable(nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.NegateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.NegateIntervalNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifference", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LocalDifference(nLocal, nLocal)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifferenceValue(expr.AsSelected(expr.Row(nLocal)), expr.AsSelected(expr.Row(nLocal))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.LocalDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.LocalDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.AsSelected(expr.Row(nMaybeLocal))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifference", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.InstantDifference(nInstant, nInstant)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifferenceValue(expr.AsSelected(expr.Row(nInstant)), expr.AsSelected(expr.Row(nInstant))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.InstantDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.InstantDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.AsSelected(expr.Row(nMaybeInstant))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifference", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ClockDifference(nClock, nClock)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceValue", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifferenceValue(expr.AsSelected(expr.Row(nClock)), expr.AsSelected(expr.Row(nClock))), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.AsSelected(expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ClockDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, expr.ClockDifferenceNullableValue(expr.AsSelected(expr.Row(nMaybeClock)), expr.AsSelected(expr.Row(nMaybeClock))), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("IntervalMonths", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalMonths(nPeriod)), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonthsValue(expr.AsSelected(expr.Row(nPeriod))), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalMonthsNullable(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.IntervalMonthsNullable(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalMonthsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalMonthsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalMonthsNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDays", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalDays(nPeriod)), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDaysValue(expr.AsSelected(expr.Row(nPeriod))), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalDaysNullable(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.AsSelected(expr.IntervalDaysNullable(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalDaysNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, expr.IntervalDaysNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalElapsed", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalElapsed(nPeriod)), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsedValue(expr.AsSelected(expr.Row(nPeriod))), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.IntervalElapsedNullable(nMaybePeriod)), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, expr.AsSelected(expr.IntervalElapsedNullable(nMaybePeriod)), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("IntervalElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.IntervalElapsedNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, expr.IntervalElapsedNullableValue(expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("ShiftDate", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftDate(nDate, nPeriod)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDateValue(expr.AsSelected(expr.Row(nDate)), expr.AsSelected(expr.Row(nPeriod))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftDateNullable(nMaybeDate, nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.ShiftDateNullable(nMaybeDate, nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftDateNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftDateNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftDateNullableValue(expr.AsSelected(expr.Row(nMaybeDate)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocal", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftLocal(nLocal, nPeriod)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocalValue(expr.AsSelected(expr.Row(nLocal)), expr.AsSelected(expr.Row(nPeriod))), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.AsSelected(expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, expr.ShiftLocalNullableValue(expr.AsSelected(expr.Row(nMaybeLocal)), expr.AsSelected(expr.Row(nMaybePeriod))), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftInstant", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftInstant(nInstant, nPeriod, expr.UTCZone())), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstantValue(expr.AsSelected(expr.Row(nInstant)), expr.AsSelected(expr.Row(nPeriod)), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone())), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.AsSelected(expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone())), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ShiftInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.ShiftInstantNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.AsSelected(expr.Row(nMaybePeriod)), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, expr.ShiftInstantNullableValue(expr.AsSelected(expr.Row(nMaybeInstant)), expr.AsSelected(expr.Row(nMaybePeriod)), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SphericalDistance", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SphericalDistance(nLatitude, nLatitude, expr.GeographicPoint{})), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceValue", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistanceValue(expr.AsSelected(expr.Row(nLatitude)), expr.AsSelected(expr.Row(nLatitude)), expr.GeographicPoint{}), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{})), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, expr.AsSelected(expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{})), codec.Nullable(codec.Float[float64]()), nil)
	})
	t.Run("SphericalDistanceNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.SphericalDistanceNullableValue(expr.AsSelected(expr.Row(nMaybeLatitude)), expr.AsSelected(expr.Row(nMaybeLatitude)), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, expr.SphericalDistanceNullableValue(expr.AsSelected(expr.Row(nMaybeLatitude)), expr.AsSelected(expr.Row(nMaybeLatitude)), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), nil)
	})
	scalarGolden(t, expr.AsSelected(expr.TransactionTime(rowset.ModelScope(scalars.Records.Query()))), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	t.Run("TrimChars", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimChars(nName, nName)), codec.String[string](), "abc")
	})
	t.Run("TrimCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimCharsValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "abc")
	})
	t.Run("TrimCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimCharsNullable(nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimCharsNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftChars", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimLeftChars(nName, nName)), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftCharsValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimLeftCharsNullable(nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimLeftCharsNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightChars", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimRightChars(nName, nName)), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightCharsValue(expr.AsSelected(expr.Row(nName)), expr.AsSelected(expr.Row(nName))), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsNullable", func(t *testing.T) {
		scalarGolden(t, expr.AsSelected(expr.TrimRightCharsNullable(nMaybeName, nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, expr.TrimRightCharsNullableValue(expr.AsSelected(expr.Row(nMaybeName)), expr.AsSelected(expr.Row(nMaybeName))), codec.Nullable(codec.String[string]()), "abc")
	})
}
