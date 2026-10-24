package goway_test

import (
	"database/sql/driver"
	"fmt"
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"testing"
	"time"
)

func scalarOldSource() query.Query[scalars.Record] {
	return query.ForModel(query.Define("scalar_records", "id", []query.Column{{Name: "id"},
		{Name: "name"}, {Name: "maybe_name", Nullable: true},
		{Name: "date"}, {Name: "maybe_date", Nullable: true},
		{Name: "local"}, {Name: "maybe_local", Nullable: true},
		{Name: "instant"}, {Name: "maybe_instant", Nullable: true},
		{Name: "clock"}, {Name: "maybe_clock", Nullable: true},
		{Name: "period"}, {Name: "maybe_period", Nullable: true},
		{Name: "millis"}, {Name: "maybe_millis", Nullable: true},
		{Name: "latitude"}, {Name: "maybe_latitude", Nullable: true},
	}, func(database.Row) (scalars.Record, error) { panic("selected projections must use their result codec") }))
}
func scalarGolden[V any](t *testing.T, ov query.Expression[scalars.Record, V], nv expr.Selected[scalars.Record, V], c codec.Codec[V], sample driver.Value) {
	t.Helper()
	before, after := &goldenCapture{}, &goldenCapture{}
	for _, g := range []*goldenCapture{before, after} {
		g.scriptedColumns = []string{"value"}
		g.scriptedBatches = [][][]driver.Value{{{sample}}}
	}
	ctx := t.Context()
	oldDB, newDB := before.open(ctx), after.open(ctx)
	defer oldDB.Close(ctx)
	defer newDB.Close(ctx)
	oldRows := must(query.SelectValue(scalarOldSource(), ov).All(ctx, oldDB))
	f := rowset.Define(func(b rowset.Builder[windowGoldenRow[V]], r *windowGoldenRow[V]) windowGoldenFields[V] {
		return windowGoldenFields[V]{b.Row(), rowset.FieldOf(b, &r.Value, orm.Codec(c))}
	})
	got := must(rowset.Select(rowset.FromModel(scalars.Records.Query()), f, rowset.MapSelected(f.Value, nv)).All(ctx, newDB))
	values := make([]V, len(got))
	for i, r := range got {
		values[i] = r.Value
	}
	fmt.Fprintln(&before.output, oldRows)
	fmt.Fprintln(&after.output, values)
	if before.statements != 1 || after.statements != 1 || before.output.String() != after.output.String() {
		t.Fatalf("scalar parity: %s", goldenDiff(before.output.String(), after.output.String()))
	}
}
func TestStage6d2ScalarGolden(t *testing.T) {
	oName := query.NewTextField[scalars.Record, string]("scalar_records", "name", codec.String[string]())
	nName := expr.Value(scalars.Records.Name)
	oMaybeName := query.NewNullableTextField[scalars.Record, string]("scalar_records", "maybe_name", codec.String[string]())
	nMaybeName := expr.Value(scalars.Records.MaybeName)
	oDate := query.NewOrderedField[scalars.Record, temporal.Date]("scalar_records", "date", codec.Date())
	nDate := expr.Value(scalars.Records.Date)
	oMaybeDate := query.NewNullableOrderedField[scalars.Record, temporal.Date]("scalar_records", "maybe_date", codec.Date())
	nMaybeDate := expr.Value(scalars.Records.MaybeDate)
	oLocal := query.NewOrderedField[scalars.Record, temporal.LocalDateTime]("scalar_records", "local", codec.LocalDateTime())
	nLocal := expr.Value(scalars.Records.Local)
	oMaybeLocal := query.NewNullableOrderedField[scalars.Record, temporal.LocalDateTime]("scalar_records", "maybe_local", codec.LocalDateTime())
	nMaybeLocal := expr.Value(scalars.Records.MaybeLocal)
	oInstant := query.NewOrderedField[scalars.Record, temporal.DateTime]("scalar_records", "instant", codec.DateTime())
	nInstant := expr.Value(scalars.Records.Instant)
	oMaybeInstant := query.NewNullableOrderedField[scalars.Record, temporal.DateTime]("scalar_records", "maybe_instant", codec.DateTime())
	nMaybeInstant := expr.Value(scalars.Records.MaybeInstant)
	oClock := query.NewOrderedField[scalars.Record, temporal.Time]("scalar_records", "clock", codec.WallTime())
	nClock := expr.Value(scalars.Records.Clock)
	oMaybeClock := query.NewNullableOrderedField[scalars.Record, temporal.Time]("scalar_records", "maybe_clock", codec.WallTime())
	nMaybeClock := expr.Value(scalars.Records.MaybeClock)
	oPeriod := query.NewOrderedField[scalars.Record, temporal.Interval]("scalar_records", "period", codec.Interval())
	nPeriod := expr.Value(scalars.Records.Period)
	oMaybePeriod := query.NewNullableOrderedField[scalars.Record, temporal.Interval]("scalar_records", "maybe_period", codec.Interval())
	nMaybePeriod := expr.Value(scalars.Records.MaybePeriod)
	oMillis := query.NewOrderedField[scalars.Record, int64]("scalar_records", "millis", codec.Signed[int64]())
	nMillis := expr.Value(scalars.Records.Millis)
	oMaybeMillis := query.NewNullableOrderedField[scalars.Record, int64]("scalar_records", "maybe_millis", codec.Signed[int64]())
	nMaybeMillis := expr.Value(scalars.Records.MaybeMillis)
	oLatitude := query.NewOrderedField[scalars.Record, float64]("scalar_records", "latitude", codec.Float[float64]())
	nLatitude := expr.Value(scalars.Records.Latitude)
	oMaybeLatitude := query.NewNullableOrderedField[scalars.Record, float64]("scalar_records", "maybe_latitude", codec.Float[float64]())
	nMaybeLatitude := expr.Value(scalars.Records.MaybeLatitude)
	t.Run("Lower", func(t *testing.T) {
		scalarGolden(t, query.Lower(oName).Value(), expr.Lower(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("LowerValue", func(t *testing.T) {
		scalarGolden(t, query.LowerValue(query.Value(oName)), expr.LowerValue(expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("LowerNullable", func(t *testing.T) {
		scalarGolden(t, query.LowerNullable(oMaybeName).Value(), expr.LowerNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.LowerNullable(oMaybeName).Value(), expr.LowerNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("LowerNullableValue", func(t *testing.T) {
		scalarGolden(t, query.LowerNullableValue(query.Value(oMaybeName)), expr.LowerNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.LowerNullableValue(query.Value(oMaybeName)), expr.LowerNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Upper", func(t *testing.T) {
		scalarGolden(t, query.Upper(oName).Value(), expr.Upper(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("UpperValue", func(t *testing.T) {
		scalarGolden(t, query.UpperValue(query.Value(oName)), expr.UpperValue(expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("UpperNullable", func(t *testing.T) {
		scalarGolden(t, query.UpperNullable(oMaybeName).Value(), expr.UpperNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.UpperNullable(oMaybeName).Value(), expr.UpperNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("UpperNullableValue", func(t *testing.T) {
		scalarGolden(t, query.UpperNullableValue(query.Value(oMaybeName)), expr.UpperNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.UpperNullableValue(query.Value(oMaybeName)), expr.UpperNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Trim", func(t *testing.T) {
		scalarGolden(t, query.Trim(oName).Value(), expr.Trim(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimValue", func(t *testing.T) {
		scalarGolden(t, query.TrimValue(query.Value(oName)), expr.TrimValue(expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimNullable(oMaybeName).Value(), expr.TrimNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimNullable(oMaybeName).Value(), expr.TrimNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimNullableValue(query.Value(oMaybeName)), expr.TrimNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimNullableValue(query.Value(oMaybeName)), expr.TrimNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeft", func(t *testing.T) {
		scalarGolden(t, query.TrimLeft(oName).Value(), expr.TrimLeft(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftValue", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftValue(query.Value(oName)), expr.TrimLeftValue(expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimLeftNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftNullable(oMaybeName).Value(), expr.TrimLeftNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimLeftNullable(oMaybeName).Value(), expr.TrimLeftNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimLeftNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftNullableValue(query.Value(oMaybeName)), expr.TrimLeftNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimLeftNullableValue(query.Value(oMaybeName)), expr.TrimLeftNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRight", func(t *testing.T) {
		scalarGolden(t, query.TrimRight(oName).Value(), expr.TrimRight(nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("TrimRightValue", func(t *testing.T) {
		scalarGolden(t, query.TrimRightValue(query.Value(oName)), expr.TrimRightValue(expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("TrimRightNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimRightNullable(oMaybeName).Value(), expr.TrimRightNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimRightNullable(oMaybeName).Value(), expr.TrimRightNullable(nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("TrimRightNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimRightNullableValue(query.Value(oMaybeName)), expr.TrimRightNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.TrimRightNullableValue(query.Value(oMaybeName)), expr.TrimRightNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Length", func(t *testing.T) {
		scalarGolden(t, query.Length(oName).Value(), expr.Length(nName).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthValue", func(t *testing.T) {
		scalarGolden(t, query.LengthValue(query.Value(oName)), expr.LengthValue(expr.Select(nName)), codec.Signed[int64](), int64(3))
	})
	t.Run("LengthNullable", func(t *testing.T) {
		scalarGolden(t, query.LengthNullable(oMaybeName).Value(), expr.LengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.LengthNullable(oMaybeName).Value(), expr.LengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("LengthNullableValue", func(t *testing.T) {
		scalarGolden(t, query.LengthNullableValue(query.Value(oMaybeName)), expr.LengthNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.LengthNullableValue(query.Value(oMaybeName)), expr.LengthNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLength", func(t *testing.T) {
		scalarGolden(t, query.OctetLength(oName).Value(), expr.OctetLength(nName).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthValue", func(t *testing.T) {
		scalarGolden(t, query.OctetLengthValue(query.Value(oName)), expr.OctetLengthValue(expr.Select(nName)), codec.Signed[int64](), int64(3))
	})
	t.Run("OctetLengthNullable", func(t *testing.T) {
		scalarGolden(t, query.OctetLengthNullable(oMaybeName).Value(), expr.OctetLengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.OctetLengthNullable(oMaybeName).Value(), expr.OctetLengthNullable(nMaybeName).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("OctetLengthNullableValue", func(t *testing.T) {
		scalarGolden(t, query.OctetLengthNullableValue(query.Value(oMaybeName)), expr.OctetLengthNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.OctetLengthNullableValue(query.Value(oMaybeName)), expr.OctetLengthNullableValue(expr.Select(nMaybeName)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Concat", func(t *testing.T) {
		scalarGolden(t, query.Concat(oName, oName).Value(), expr.Concat(nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatValue", func(t *testing.T) {
		scalarGolden(t, query.ConcatValue(query.Value(oName), query.Value(oName)), expr.ConcatValue(expr.Select(nName), expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("ConcatNullable", func(t *testing.T) {
		scalarGolden(t, query.ConcatNullable(oMaybeName, oMaybeName).Value(), expr.ConcatNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.ConcatNullable(oMaybeName, oMaybeName).Value(), expr.ConcatNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ConcatNullableValue(query.Value(oMaybeName), query.Value(oMaybeName)), expr.ConcatNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.ConcatNullableValue(query.Value(oMaybeName), query.Value(oMaybeName)), expr.ConcatNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Replace", func(t *testing.T) {
		scalarGolden(t, query.Replace(oName, oName, oName).Value(), expr.Replace(nName, nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ReplaceValue", func(t *testing.T) {
		scalarGolden(t, query.ReplaceValue(query.Value(oName), query.Value(oName), query.Value(oName)), expr.ReplaceValue(expr.Select(nName), expr.Select(nName), expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("ReplaceNullable", func(t *testing.T) {
		scalarGolden(t, query.ReplaceNullable(oMaybeName, oMaybeName, oMaybeName).Value(), expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.ReplaceNullable(oMaybeName, oMaybeName, oMaybeName).Value(), expr.ReplaceNullable(nMaybeName, nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ReplaceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ReplaceNullableValue(query.Value(oMaybeName), query.Value(oMaybeName), query.Value(oMaybeName)), expr.ReplaceNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.ReplaceNullableValue(query.Value(oMaybeName), query.Value(oMaybeName), query.Value(oMaybeName)), expr.ReplaceNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("ConcatWS", func(t *testing.T) {
		scalarGolden(t, query.ConcatWS("|", oName, oName).Value(), expr.ConcatWS("|", nName, nName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSValue", func(t *testing.T) {
		scalarGolden(t, query.ConcatWSValue("|", query.Value(oName), query.Value(oName)), expr.ConcatWSValue("|", expr.Select(nName), expr.Select(nName)), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullable", func(t *testing.T) {
		scalarGolden(t, query.ConcatWSNullable("|", oMaybeName, oMaybeName).Value(), expr.ConcatWSNullable("|", nMaybeName, nMaybeName).Value(), codec.String[string](), "héllo")
	})
	t.Run("ConcatWSNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ConcatWSNullableValue("|", query.Value(oMaybeName), query.Value(oMaybeName)), expr.ConcatWSNullableValue("|", expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.String[string](), "héllo")
	})
	t.Run("Substring", func(t *testing.T) {
		scalarGolden(t, query.Substring(oName, int32(2), int32(3)).Value(), expr.Substring(nName, int32(2), int32(3)).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringValue(query.Value(oName), int32(2), int32(3)), expr.SubstringValue(expr.Select(nName), int32(2), int32(3)), codec.String[string](), "héllo")
	})
	t.Run("SubstringNullable", func(t *testing.T) {
		scalarGolden(t, query.SubstringNullable(oMaybeName, int32(2), int32(3)).Value(), expr.SubstringNullable(nMaybeName, int32(2), int32(3)).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringNullable(oMaybeName, int32(2), int32(3)).Value(), expr.SubstringNullable(nMaybeName, int32(2), int32(3)).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringNullableValue(query.Value(oMaybeName), int32(2), int32(3)), expr.SubstringNullableValue(expr.Select(nMaybeName), int32(2), int32(3)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringNullableValue(query.Value(oMaybeName), int32(2), int32(3)), expr.SubstringNullableValue(expr.Select(nMaybeName), int32(2), int32(3)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFrom", func(t *testing.T) {
		scalarGolden(t, query.SubstringFrom(oName, int32(2)).Value(), expr.SubstringFrom(nName, int32(2)).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringFromValue(query.Value(oName), int32(2)), expr.SubstringFromValue(expr.Select(nName), int32(2)), codec.String[string](), "héllo")
	})
	t.Run("SubstringFromNullable", func(t *testing.T) {
		scalarGolden(t, query.SubstringFromNullable(oMaybeName, int32(2)).Value(), expr.SubstringFromNullable(nMaybeName, int32(2)).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringFromNullable(oMaybeName, int32(2)).Value(), expr.SubstringFromNullable(nMaybeName, int32(2)).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringFromNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringFromNullableValue(query.Value(oMaybeName), int32(2)), expr.SubstringFromNullableValue(expr.Select(nMaybeName), int32(2)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringFromNullableValue(query.Value(oMaybeName), int32(2)), expr.SubstringFromNullableValue(expr.Select(nMaybeName), int32(2)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAt", func(t *testing.T) {
		scalarGolden(t, query.SubstringAt(oName, oMillis, oMillis).Value(), expr.SubstringAt(nName, nMillis, nMillis).Value(), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringAtValue(query.Value(oName), query.Value(oMillis), query.Value(oMillis)), expr.SubstringAtValue(expr.Select(nName), expr.Select(nMillis), expr.Select(nMillis)), codec.String[string](), "héllo")
	})
	t.Run("SubstringAtNullable", func(t *testing.T) {
		scalarGolden(t, query.SubstringAtNullable(oMaybeName, oMillis, oMillis).Value(), expr.SubstringAtNullable(nMaybeName, nMillis, nMillis).Value(), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringAtNullable(oMaybeName, oMillis, oMillis).Value(), expr.SubstringAtNullable(nMaybeName, nMillis, nMillis).Value(), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("SubstringAtNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubstringAtNullableValue(query.Value(oMaybeName), query.Value(oMillis), query.Value(oMillis)), expr.SubstringAtNullableValue(expr.Select(nMaybeName), expr.Select(nMillis), expr.Select(nMillis)), codec.Nullable(codec.String[string]()), "héllo")
		scalarGolden(t, query.SubstringAtNullableValue(query.Value(oMaybeName), query.Value(oMillis), query.Value(oMillis)), expr.SubstringAtNullableValue(expr.Select(nMaybeName), expr.Select(nMillis), expr.Select(nMillis)), codec.Nullable(codec.String[string]()), nil)
	})
	t.Run("Year", func(t *testing.T) {
		scalarGolden(t, query.Year(oDate).Value(), expr.Year(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("YearValue", func(t *testing.T) {
		scalarGolden(t, query.YearValue(query.Value(oDate)), expr.YearValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("YearNullable", func(t *testing.T) {
		scalarGolden(t, query.YearNullable(oMaybeDate).Value(), expr.YearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.YearNullable(oMaybeDate).Value(), expr.YearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("YearNullableValue", func(t *testing.T) {
		scalarGolden(t, query.YearNullableValue(query.Value(oMaybeDate)), expr.YearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.YearNullableValue(query.Value(oMaybeDate)), expr.YearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Month", func(t *testing.T) {
		scalarGolden(t, query.Month(oDate).Value(), expr.Month(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthValue", func(t *testing.T) {
		scalarGolden(t, query.MonthValue(query.Value(oDate)), expr.MonthValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("MonthNullable", func(t *testing.T) {
		scalarGolden(t, query.MonthNullable(oMaybeDate).Value(), expr.MonthNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MonthNullable(oMaybeDate).Value(), expr.MonthNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MonthNullableValue", func(t *testing.T) {
		scalarGolden(t, query.MonthNullableValue(query.Value(oMaybeDate)), expr.MonthNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MonthNullableValue(query.Value(oMaybeDate)), expr.MonthNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Day", func(t *testing.T) {
		scalarGolden(t, query.Day(oDate).Value(), expr.Day(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayValue", func(t *testing.T) {
		scalarGolden(t, query.DayValue(query.Value(oDate)), expr.DayValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayNullable", func(t *testing.T) {
		scalarGolden(t, query.DayNullable(oMaybeDate).Value(), expr.DayNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayNullable(oMaybeDate).Value(), expr.DayNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DayNullableValue(query.Value(oMaybeDate)), expr.DayNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayNullableValue(query.Value(oMaybeDate)), expr.DayNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeek", func(t *testing.T) {
		scalarGolden(t, query.DayOfWeek(oDate).Value(), expr.DayOfWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, query.DayOfWeekValue(query.Value(oDate)), expr.DayOfWeekValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, query.DayOfWeekNullable(oMaybeDate).Value(), expr.DayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayOfWeekNullable(oMaybeDate).Value(), expr.DayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DayOfWeekNullableValue(query.Value(oMaybeDate)), expr.DayOfWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayOfWeekNullableValue(query.Value(oMaybeDate)), expr.DayOfWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeek", func(t *testing.T) {
		scalarGolden(t, query.ISODayOfWeek(oDate).Value(), expr.ISODayOfWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekValue", func(t *testing.T) {
		scalarGolden(t, query.ISODayOfWeekValue(query.Value(oDate)), expr.ISODayOfWeekValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISODayOfWeekNullable", func(t *testing.T) {
		scalarGolden(t, query.ISODayOfWeekNullable(oMaybeDate).Value(), expr.ISODayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISODayOfWeekNullable(oMaybeDate).Value(), expr.ISODayOfWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISODayOfWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ISODayOfWeekNullableValue(query.Value(oMaybeDate)), expr.ISODayOfWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISODayOfWeekNullableValue(query.Value(oMaybeDate)), expr.ISODayOfWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYear", func(t *testing.T) {
		scalarGolden(t, query.DayOfYear(oDate).Value(), expr.DayOfYear(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearValue", func(t *testing.T) {
		scalarGolden(t, query.DayOfYearValue(query.Value(oDate)), expr.DayOfYearValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DayOfYearNullable", func(t *testing.T) {
		scalarGolden(t, query.DayOfYearNullable(oMaybeDate).Value(), expr.DayOfYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayOfYearNullable(oMaybeDate).Value(), expr.DayOfYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DayOfYearNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DayOfYearNullableValue(query.Value(oMaybeDate)), expr.DayOfYearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DayOfYearNullableValue(query.Value(oMaybeDate)), expr.DayOfYearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeek", func(t *testing.T) {
		scalarGolden(t, query.ISOWeek(oDate).Value(), expr.ISOWeek(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekValue", func(t *testing.T) {
		scalarGolden(t, query.ISOWeekValue(query.Value(oDate)), expr.ISOWeekValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOWeekNullable", func(t *testing.T) {
		scalarGolden(t, query.ISOWeekNullable(oMaybeDate).Value(), expr.ISOWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISOWeekNullable(oMaybeDate).Value(), expr.ISOWeekNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOWeekNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ISOWeekNullableValue(query.Value(oMaybeDate)), expr.ISOWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISOWeekNullableValue(query.Value(oMaybeDate)), expr.ISOWeekNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYear", func(t *testing.T) {
		scalarGolden(t, query.ISOYear(oDate).Value(), expr.ISOYear(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearValue", func(t *testing.T) {
		scalarGolden(t, query.ISOYearValue(query.Value(oDate)), expr.ISOYearValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("ISOYearNullable", func(t *testing.T) {
		scalarGolden(t, query.ISOYearNullable(oMaybeDate).Value(), expr.ISOYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISOYearNullable(oMaybeDate).Value(), expr.ISOYearNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("ISOYearNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ISOYearNullableValue(query.Value(oMaybeDate)), expr.ISOYearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.ISOYearNullableValue(query.Value(oMaybeDate)), expr.ISOYearNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Quarter", func(t *testing.T) {
		scalarGolden(t, query.Quarter(oDate).Value(), expr.Quarter(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterValue", func(t *testing.T) {
		scalarGolden(t, query.QuarterValue(query.Value(oDate)), expr.QuarterValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("QuarterNullable", func(t *testing.T) {
		scalarGolden(t, query.QuarterNullable(oMaybeDate).Value(), expr.QuarterNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.QuarterNullable(oMaybeDate).Value(), expr.QuarterNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("QuarterNullableValue", func(t *testing.T) {
		scalarGolden(t, query.QuarterNullableValue(query.Value(oMaybeDate)), expr.QuarterNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.QuarterNullableValue(query.Value(oMaybeDate)), expr.QuarterNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Century", func(t *testing.T) {
		scalarGolden(t, query.Century(oDate).Value(), expr.Century(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyValue", func(t *testing.T) {
		scalarGolden(t, query.CenturyValue(query.Value(oDate)), expr.CenturyValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("CenturyNullable", func(t *testing.T) {
		scalarGolden(t, query.CenturyNullable(oMaybeDate).Value(), expr.CenturyNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.CenturyNullable(oMaybeDate).Value(), expr.CenturyNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("CenturyNullableValue", func(t *testing.T) {
		scalarGolden(t, query.CenturyNullableValue(query.Value(oMaybeDate)), expr.CenturyNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.CenturyNullableValue(query.Value(oMaybeDate)), expr.CenturyNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Decade", func(t *testing.T) {
		scalarGolden(t, query.Decade(oDate).Value(), expr.Decade(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeValue", func(t *testing.T) {
		scalarGolden(t, query.DecadeValue(query.Value(oDate)), expr.DecadeValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DecadeNullable", func(t *testing.T) {
		scalarGolden(t, query.DecadeNullable(oMaybeDate).Value(), expr.DecadeNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DecadeNullable(oMaybeDate).Value(), expr.DecadeNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DecadeNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DecadeNullableValue(query.Value(oMaybeDate)), expr.DecadeNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DecadeNullableValue(query.Value(oMaybeDate)), expr.DecadeNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Millennium", func(t *testing.T) {
		scalarGolden(t, query.Millennium(oDate).Value(), expr.Millennium(nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumValue", func(t *testing.T) {
		scalarGolden(t, query.MillenniumValue(query.Value(oDate)), expr.MillenniumValue(expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("MillenniumNullable", func(t *testing.T) {
		scalarGolden(t, query.MillenniumNullable(oMaybeDate).Value(), expr.MillenniumNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MillenniumNullable(oMaybeDate).Value(), expr.MillenniumNullable(nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MillenniumNullableValue", func(t *testing.T) {
		scalarGolden(t, query.MillenniumNullableValue(query.Value(oMaybeDate)), expr.MillenniumNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MillenniumNullableValue(query.Value(oMaybeDate)), expr.MillenniumNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("JulianDay", func(t *testing.T) {
		scalarGolden(t, query.JulianDay(oDate).Value(), expr.JulianDay(nDate).Value(), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayValue", func(t *testing.T) {
		scalarGolden(t, query.JulianDayValue(query.Value(oDate)), expr.JulianDayValue(expr.Select(nDate)), codec.Decimal(), "12.34")
	})
	t.Run("JulianDayNullable", func(t *testing.T) {
		scalarGolden(t, query.JulianDayNullable(oMaybeDate).Value(), expr.JulianDayNullable(nMaybeDate).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.JulianDayNullable(oMaybeDate).Value(), expr.JulianDayNullable(nMaybeDate).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("JulianDayNullableValue", func(t *testing.T) {
		scalarGolden(t, query.JulianDayNullableValue(query.Value(oMaybeDate)), expr.JulianDayNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.JulianDayNullableValue(query.Value(oMaybeDate)), expr.JulianDayNullableValue(expr.Select(nMaybeDate)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Hour", func(t *testing.T) {
		scalarGolden(t, query.Hour(oClock).Value(), expr.Hour(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("HourValue", func(t *testing.T) {
		scalarGolden(t, query.HourValue(query.Value(oClock)), expr.HourValue(expr.Select(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("HourNullable", func(t *testing.T) {
		scalarGolden(t, query.HourNullable(oMaybeClock).Value(), expr.HourNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.HourNullable(oMaybeClock).Value(), expr.HourNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("HourNullableValue", func(t *testing.T) {
		scalarGolden(t, query.HourNullableValue(query.Value(oMaybeClock)), expr.HourNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.HourNullableValue(query.Value(oMaybeClock)), expr.HourNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Minute", func(t *testing.T) {
		scalarGolden(t, query.Minute(oClock).Value(), expr.Minute(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteValue", func(t *testing.T) {
		scalarGolden(t, query.MinuteValue(query.Value(oClock)), expr.MinuteValue(expr.Select(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("MinuteNullable", func(t *testing.T) {
		scalarGolden(t, query.MinuteNullable(oMaybeClock).Value(), expr.MinuteNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MinuteNullable(oMaybeClock).Value(), expr.MinuteNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MinuteNullableValue", func(t *testing.T) {
		scalarGolden(t, query.MinuteNullableValue(query.Value(oMaybeClock)), expr.MinuteNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MinuteNullableValue(query.Value(oMaybeClock)), expr.MinuteNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Microseconds", func(t *testing.T) {
		scalarGolden(t, query.Microseconds(oClock).Value(), expr.Microseconds(nClock).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsValue", func(t *testing.T) {
		scalarGolden(t, query.MicrosecondsValue(query.Value(oClock)), expr.MicrosecondsValue(expr.Select(nClock)), codec.Signed[int64](), int64(3))
	})
	t.Run("MicrosecondsNullable", func(t *testing.T) {
		scalarGolden(t, query.MicrosecondsNullable(oMaybeClock).Value(), expr.MicrosecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MicrosecondsNullable(oMaybeClock).Value(), expr.MicrosecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("MicrosecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.MicrosecondsNullableValue(query.Value(oMaybeClock)), expr.MicrosecondsNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.MicrosecondsNullableValue(query.Value(oMaybeClock)), expr.MicrosecondsNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("Second", func(t *testing.T) {
		scalarGolden(t, query.Second(oClock).Value(), expr.Second(nClock).Value(), codec.Decimal(), "12.34")
	})
	t.Run("SecondValue", func(t *testing.T) {
		scalarGolden(t, query.SecondValue(query.Value(oClock)), expr.SecondValue(expr.Select(nClock)), codec.Decimal(), "12.34")
	})
	t.Run("SecondNullable", func(t *testing.T) {
		scalarGolden(t, query.SecondNullable(oMaybeClock).Value(), expr.SecondNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.SecondNullable(oMaybeClock).Value(), expr.SecondNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("SecondNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SecondNullableValue(query.Value(oMaybeClock)), expr.SecondNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.SecondNullableValue(query.Value(oMaybeClock)), expr.SecondNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("Milliseconds", func(t *testing.T) {
		scalarGolden(t, query.Milliseconds(oClock).Value(), expr.Milliseconds(nClock).Value(), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsValue", func(t *testing.T) {
		scalarGolden(t, query.MillisecondsValue(query.Value(oClock)), expr.MillisecondsValue(expr.Select(nClock)), codec.Decimal(), "12.34")
	})
	t.Run("MillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, query.MillisecondsNullable(oMaybeClock).Value(), expr.MillisecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.MillisecondsNullable(oMaybeClock).Value(), expr.MillisecondsNullable(nMaybeClock).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("MillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.MillisecondsNullableValue(query.Value(oMaybeClock)), expr.MillisecondsNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.MillisecondsNullableValue(query.Value(oMaybeClock)), expr.MillisecondsNullableValue(expr.Select(nMaybeClock)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSeconds", func(t *testing.T) {
		scalarGolden(t, query.UnixSeconds(oInstant).Value(), expr.UnixSeconds(nInstant).Value(), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsValue", func(t *testing.T) {
		scalarGolden(t, query.UnixSecondsValue(query.Value(oInstant)), expr.UnixSecondsValue(expr.Select(nInstant)), codec.Decimal(), "12.34")
	})
	t.Run("UnixSecondsNullable", func(t *testing.T) {
		scalarGolden(t, query.UnixSecondsNullable(oMaybeInstant).Value(), expr.UnixSecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.UnixSecondsNullable(oMaybeInstant).Value(), expr.UnixSecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixSecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.UnixSecondsNullableValue(query.Value(oMaybeInstant)), expr.UnixSecondsNullableValue(expr.Select(nMaybeInstant)), codec.Nullable(codec.Decimal()), "12.34")
		scalarGolden(t, query.UnixSecondsNullableValue(query.Value(oMaybeInstant)), expr.UnixSecondsNullableValue(expr.Select(nMaybeInstant)), codec.Nullable(codec.Decimal()), nil)
	})
	t.Run("UnixMilliseconds", func(t *testing.T) {
		scalarGolden(t, query.UnixMilliseconds(oInstant).Value(), expr.UnixMilliseconds(nInstant).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsValue", func(t *testing.T) {
		scalarGolden(t, query.UnixMillisecondsValue(query.Value(oInstant)), expr.UnixMillisecondsValue(expr.Select(nInstant)), codec.Signed[int64](), int64(3))
	})
	t.Run("UnixMillisecondsNullable", func(t *testing.T) {
		scalarGolden(t, query.UnixMillisecondsNullable(oMaybeInstant).Value(), expr.UnixMillisecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.UnixMillisecondsNullable(oMaybeInstant).Value(), expr.UnixMillisecondsNullable(nMaybeInstant).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("UnixMillisecondsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.UnixMillisecondsNullableValue(query.Value(oMaybeInstant)), expr.UnixMillisecondsNullableValue(expr.Select(nMaybeInstant)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.UnixMillisecondsNullableValue(query.Value(oMaybeInstant)), expr.UnixMillisecondsNullableValue(expr.Select(nMaybeInstant)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("TruncateDate", func(t *testing.T) {
		scalarGolden(t, query.TruncateDate(oDate, query.DateMonth).Value(), expr.TruncateDate(nDate, expr.DateMonth).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateDateValue(query.Value(oDate), query.DateMonth), expr.TruncateDateValue(expr.Select(nDate), expr.DateMonth), codec.Date(), "2024-02-29")
	})
	t.Run("TruncateDateNullable", func(t *testing.T) {
		scalarGolden(t, query.TruncateDateNullable(oMaybeDate, query.DateMonth).Value(), expr.TruncateDateNullable(nMaybeDate, expr.DateMonth).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.TruncateDateNullable(oMaybeDate, query.DateMonth).Value(), expr.TruncateDateNullable(nMaybeDate, expr.DateMonth).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateDateNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateDateNullableValue(query.Value(oMaybeDate), query.DateMonth), expr.TruncateDateNullableValue(expr.Select(nMaybeDate), expr.DateMonth), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.TruncateDateNullableValue(query.Value(oMaybeDate), query.DateMonth), expr.TruncateDateNullableValue(expr.Select(nMaybeDate), expr.DateMonth), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TruncateLocal", func(t *testing.T) {
		scalarGolden(t, query.TruncateLocal(oLocal, query.TimestampHour).Value(), expr.TruncateLocal(nLocal, expr.TimestampHour).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateLocalValue(query.Value(oLocal), query.TimestampHour), expr.TruncateLocalValue(expr.Select(nLocal), expr.TimestampHour), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("TruncateLocalNullable", func(t *testing.T) {
		scalarGolden(t, query.TruncateLocalNullable(oMaybeLocal, query.TimestampHour).Value(), expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.TruncateLocalNullable(oMaybeLocal, query.TimestampHour).Value(), expr.TruncateLocalNullable(nMaybeLocal, expr.TimestampHour).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateLocalNullableValue(query.Value(oMaybeLocal), query.TimestampHour), expr.TruncateLocalNullableValue(expr.Select(nMaybeLocal), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.TruncateLocalNullableValue(query.Value(oMaybeLocal), query.TimestampHour), expr.TruncateLocalNullableValue(expr.Select(nMaybeLocal), expr.TimestampHour), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("TruncateInstant", func(t *testing.T) {
		scalarGolden(t, query.TruncateInstant(oInstant, query.TimestampDay, query.UTCZone()).Value(), expr.TruncateInstant(nInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateInstantValue(query.Value(oInstant), query.TimestampDay, query.UTCZone()), expr.TruncateInstantValue(expr.Select(nInstant), expr.TimestampDay, expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("TruncateInstantNullable", func(t *testing.T) {
		scalarGolden(t, query.TruncateInstantNullable(oMaybeInstant, query.TimestampDay, query.UTCZone()).Value(), expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.TruncateInstantNullable(oMaybeInstant, query.TimestampDay, query.UTCZone()).Value(), expr.TruncateInstantNullable(nMaybeInstant, expr.TimestampDay, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("TruncateInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TruncateInstantNullableValue(query.Value(oMaybeInstant), query.TimestampDay, query.UTCZone()), expr.TruncateInstantNullableValue(expr.Select(nMaybeInstant), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.TruncateInstantNullableValue(query.Value(oMaybeInstant), query.TimestampDay, query.UTCZone()), expr.TruncateInstantNullableValue(expr.Select(nMaybeInstant), expr.TimestampDay, expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillis", func(t *testing.T) {
		scalarGolden(t, query.FromUnixMillis(oMillis).Value(), expr.FromUnixMillis(nMillis).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisValue", func(t *testing.T) {
		scalarGolden(t, query.FromUnixMillisValue(query.Value(oMillis)), expr.FromUnixMillisValue(expr.Select(nMillis)), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("FromUnixMillisNullable", func(t *testing.T) {
		scalarGolden(t, query.FromUnixMillisNullable(oMaybeMillis).Value(), expr.FromUnixMillisNullable(nMaybeMillis).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.FromUnixMillisNullable(oMaybeMillis).Value(), expr.FromUnixMillisNullable(nMaybeMillis).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("FromUnixMillisNullableValue", func(t *testing.T) {
		scalarGolden(t, query.FromUnixMillisNullableValue(query.Value(oMaybeMillis)), expr.FromUnixMillisNullableValue(expr.Select(nMaybeMillis)), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.FromUnixMillisNullableValue(query.Value(oMaybeMillis)), expr.FromUnixMillisNullableValue(expr.Select(nMaybeMillis)), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("LocalAt", func(t *testing.T) {
		scalarGolden(t, query.LocalAt(oInstant, query.UTCZone()).Value(), expr.LocalAt(nInstant, expr.UTCZone()).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtValue", func(t *testing.T) {
		scalarGolden(t, query.LocalAtValue(query.Value(oInstant), query.UTCZone()), expr.LocalAtValue(expr.Select(nInstant), expr.UTCZone()), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("LocalAtNullable", func(t *testing.T) {
		scalarGolden(t, query.LocalAtNullable(oMaybeInstant, query.UTCZone()).Value(), expr.LocalAtNullable(nMaybeInstant, expr.UTCZone()).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.LocalAtNullable(oMaybeInstant, query.UTCZone()).Value(), expr.LocalAtNullable(nMaybeInstant, expr.UTCZone()).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("LocalAtNullableValue", func(t *testing.T) {
		scalarGolden(t, query.LocalAtNullableValue(query.Value(oMaybeInstant), query.UTCZone()), expr.LocalAtNullableValue(expr.Select(nMaybeInstant), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.LocalAtNullableValue(query.Value(oMaybeInstant), query.UTCZone()), expr.LocalAtNullableValue(expr.Select(nMaybeInstant), expr.UTCZone()), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ResolveLocal", func(t *testing.T) {
		scalarGolden(t, query.ResolveLocal(oLocal, query.UTCZone(), query.PostgresStandardTime).Value(), expr.ResolveLocal(nLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalValue", func(t *testing.T) {
		scalarGolden(t, query.ResolveLocalValue(query.Value(oLocal), query.UTCZone(), query.PostgresStandardTime), expr.ResolveLocalValue(expr.Select(nLocal), expr.UTCZone(), expr.PostgresStandardTime), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ResolveLocalNullable", func(t *testing.T) {
		scalarGolden(t, query.ResolveLocalNullable(oMaybeLocal, query.UTCZone(), query.PostgresStandardTime).Value(), expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.ResolveLocalNullable(oMaybeLocal, query.UTCZone(), query.PostgresStandardTime).Value(), expr.ResolveLocalNullable(nMaybeLocal, expr.UTCZone(), expr.PostgresStandardTime).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ResolveLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ResolveLocalNullableValue(query.Value(oMaybeLocal), query.UTCZone(), query.PostgresStandardTime), expr.ResolveLocalNullableValue(expr.Select(nMaybeLocal), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.ResolveLocalNullableValue(query.Value(oMaybeLocal), query.UTCZone(), query.PostgresStandardTime), expr.ResolveLocalNullableValue(expr.Select(nMaybeLocal), expr.UTCZone(), expr.PostgresStandardTime), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("DateOf", func(t *testing.T) {
		scalarGolden(t, query.DateOf(oLocal).Value(), expr.DateOf(nLocal).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfValue", func(t *testing.T) {
		scalarGolden(t, query.DateOfValue(query.Value(oLocal)), expr.DateOfValue(expr.Select(nLocal)), codec.Date(), "2024-02-29")
	})
	t.Run("DateOfNullable", func(t *testing.T) {
		scalarGolden(t, query.DateOfNullable(oMaybeLocal).Value(), expr.DateOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.DateOfNullable(oMaybeLocal).Value(), expr.DateOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateOfNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DateOfNullableValue(query.Value(oMaybeLocal)), expr.DateOfNullableValue(expr.Select(nMaybeLocal)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.DateOfNullableValue(query.Value(oMaybeLocal)), expr.DateOfNullableValue(expr.Select(nMaybeLocal)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("TimeOf", func(t *testing.T) {
		scalarGolden(t, query.TimeOf(oLocal).Value(), expr.TimeOf(nLocal).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfValue", func(t *testing.T) {
		scalarGolden(t, query.TimeOfValue(query.Value(oLocal)), expr.TimeOfValue(expr.Select(nLocal)), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("TimeOfNullable", func(t *testing.T) {
		scalarGolden(t, query.TimeOfNullable(oMaybeLocal).Value(), expr.TimeOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.TimeOfNullable(oMaybeLocal).Value(), expr.TimeOfNullable(nMaybeLocal).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("TimeOfNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TimeOfNullableValue(query.Value(oMaybeLocal)), expr.TimeOfNullableValue(expr.Select(nMaybeLocal)), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.TimeOfNullableValue(query.Value(oMaybeLocal)), expr.TimeOfNullableValue(expr.Select(nMaybeLocal)), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("CombineDateTime", func(t *testing.T) {
		scalarGolden(t, query.CombineDateTime(oDate, oClock).Value(), expr.CombineDateTime(nDate, nClock).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeValue", func(t *testing.T) {
		scalarGolden(t, query.CombineDateTimeValue(query.Value(oDate), query.Value(oClock)), expr.CombineDateTimeValue(expr.Select(nDate), expr.Select(nClock)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("CombineDateTimeNullable", func(t *testing.T) {
		scalarGolden(t, query.CombineDateTimeNullable(oMaybeDate, oMaybeClock).Value(), expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.CombineDateTimeNullable(oMaybeDate, oMaybeClock).Value(), expr.CombineDateTimeNullable(nMaybeDate, nMaybeClock).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("CombineDateTimeNullableValue", func(t *testing.T) {
		scalarGolden(t, query.CombineDateTimeNullableValue(query.Value(oMaybeDate), query.Value(oMaybeClock)), expr.CombineDateTimeNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybeClock)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.CombineDateTimeNullableValue(query.Value(oMaybeDate), query.Value(oMaybeClock)), expr.CombineDateTimeNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybeClock)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateDays", func(t *testing.T) {
		scalarGolden(t, query.AddDateDays(oDate, int32(2)).Value(), expr.AddDateDays(nDate, int32(2)).Value(), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysValue", func(t *testing.T) {
		scalarGolden(t, query.AddDateDaysValue(query.Value(oDate), int32(2)), expr.AddDateDaysValue(expr.Select(nDate), int32(2)), codec.Date(), "2024-02-29")
	})
	t.Run("AddDateDaysNullable", func(t *testing.T) {
		scalarGolden(t, query.AddDateDaysNullable(oMaybeDate, int32(2)).Value(), expr.AddDateDaysNullable(nMaybeDate, int32(2)).Value(), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.AddDateDaysNullable(oMaybeDate, int32(2)).Value(), expr.AddDateDaysNullable(nMaybeDate, int32(2)).Value(), codec.Nullable(codec.Date()), nil)
	})
	t.Run("AddDateDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddDateDaysNullableValue(query.Value(oMaybeDate), int32(2)), expr.AddDateDaysNullableValue(expr.Select(nMaybeDate), int32(2)), codec.Nullable(codec.Date()), "2024-02-29")
		scalarGolden(t, query.AddDateDaysNullableValue(query.Value(oMaybeDate), int32(2)), expr.AddDateDaysNullableValue(expr.Select(nMaybeDate), int32(2)), codec.Nullable(codec.Date()), nil)
	})
	t.Run("DateDifference", func(t *testing.T) {
		scalarGolden(t, query.DateDifference(oDate, oDate).Value(), expr.DateDifference(nDate, nDate).Value(), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceValue", func(t *testing.T) {
		scalarGolden(t, query.DateDifferenceValue(query.Value(oDate), query.Value(oDate)), expr.DateDifferenceValue(expr.Select(nDate), expr.Select(nDate)), codec.Signed[int64](), int64(3))
	})
	t.Run("DateDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, query.DateDifferenceNullable(oMaybeDate, oMaybeDate).Value(), expr.DateDifferenceNullable(nMaybeDate, nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DateDifferenceNullable(oMaybeDate, oMaybeDate).Value(), expr.DateDifferenceNullable(nMaybeDate, nMaybeDate).Value(), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("DateDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.DateDifferenceNullableValue(query.Value(oMaybeDate), query.Value(oMaybeDate)), expr.DateDifferenceNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), int64(3))
		scalarGolden(t, query.DateDifferenceNullableValue(query.Value(oMaybeDate), query.Value(oMaybeDate)), expr.DateDifferenceNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybeDate)), codec.Nullable(codec.Signed[int64]()), nil)
	})
	t.Run("AddDateInterval", func(t *testing.T) {
		scalarGolden(t, query.AddDateInterval(oDate, temporal.Days(2)).Value(), expr.AddDateInterval(nDate, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.AddDateIntervalValue(query.Value(oDate), temporal.Days(2)), expr.AddDateIntervalValue(expr.Select(nDate), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.AddDateIntervalNullable(oMaybeDate, temporal.Days(2)).Value(), expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.AddDateIntervalNullable(oMaybeDate, temporal.Days(2)).Value(), expr.AddDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddDateIntervalNullableValue(query.Value(oMaybeDate), temporal.Days(2)), expr.AddDateIntervalNullableValue(expr.Select(nMaybeDate), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.AddDateIntervalNullableValue(query.Value(oMaybeDate), temporal.Days(2)), expr.AddDateIntervalNullableValue(expr.Select(nMaybeDate), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalInterval", func(t *testing.T) {
		scalarGolden(t, query.AddLocalInterval(oLocal, temporal.Days(2)).Value(), expr.AddLocalInterval(nLocal, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.AddLocalIntervalValue(query.Value(oLocal), temporal.Days(2)), expr.AddLocalIntervalValue(expr.Select(nLocal), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("AddLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.AddLocalIntervalNullable(oMaybeLocal, temporal.Days(2)).Value(), expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.AddLocalIntervalNullable(oMaybeLocal, temporal.Days(2)).Value(), expr.AddLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddLocalIntervalNullableValue(query.Value(oMaybeLocal), temporal.Days(2)), expr.AddLocalIntervalNullableValue(expr.Select(nMaybeLocal), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.AddLocalIntervalNullableValue(query.Value(oMaybeLocal), temporal.Days(2)), expr.AddLocalIntervalNullableValue(expr.Select(nMaybeLocal), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("AddInstantInterval", func(t *testing.T) {
		scalarGolden(t, query.AddInstantInterval(oInstant, temporal.Days(2), query.UTCZone()).Value(), expr.AddInstantInterval(nInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.AddInstantIntervalValue(query.Value(oInstant), temporal.Days(2), query.UTCZone()), expr.AddInstantIntervalValue(expr.Select(nInstant), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("AddInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.AddInstantIntervalNullable(oMaybeInstant, temporal.Days(2), query.UTCZone()).Value(), expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.AddInstantIntervalNullable(oMaybeInstant, temporal.Days(2), query.UTCZone()).Value(), expr.AddInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddInstantIntervalNullableValue(query.Value(oMaybeInstant), temporal.Days(2), query.UTCZone()), expr.AddInstantIntervalNullableValue(expr.Select(nMaybeInstant), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.AddInstantIntervalNullableValue(query.Value(oMaybeInstant), temporal.Days(2), query.UTCZone()), expr.AddInstantIntervalNullableValue(expr.Select(nMaybeInstant), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("AddClockElapsed", func(t *testing.T) {
		scalarGolden(t, query.AddClockElapsed(oClock, time.Second).Value(), expr.AddClockElapsed(nClock, time.Second).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, query.AddClockElapsedValue(query.Value(oClock), time.Second), expr.AddClockElapsedValue(expr.Select(nClock), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("AddClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, query.AddClockElapsedNullable(oMaybeClock, time.Second).Value(), expr.AddClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.AddClockElapsedNullable(oMaybeClock, time.Second).Value(), expr.AddClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddClockElapsedNullableValue(query.Value(oMaybeClock), time.Second), expr.AddClockElapsedNullableValue(expr.Select(nMaybeClock), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.AddClockElapsedNullableValue(query.Value(oMaybeClock), time.Second), expr.AddClockElapsedNullableValue(expr.Select(nMaybeClock), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractDateInterval", func(t *testing.T) {
		scalarGolden(t, query.SubtractDateInterval(oDate, temporal.Days(2)).Value(), expr.SubtractDateInterval(nDate, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractDateIntervalValue(query.Value(oDate), temporal.Days(2)), expr.SubtractDateIntervalValue(expr.Select(nDate), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractDateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.SubtractDateIntervalNullable(oMaybeDate, temporal.Days(2)).Value(), expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.SubtractDateIntervalNullable(oMaybeDate, temporal.Days(2)).Value(), expr.SubtractDateIntervalNullable(nMaybeDate, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractDateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractDateIntervalNullableValue(query.Value(oMaybeDate), temporal.Days(2)), expr.SubtractDateIntervalNullableValue(expr.Select(nMaybeDate), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.SubtractDateIntervalNullableValue(query.Value(oMaybeDate), temporal.Days(2)), expr.SubtractDateIntervalNullableValue(expr.Select(nMaybeDate), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalInterval", func(t *testing.T) {
		scalarGolden(t, query.SubtractLocalInterval(oLocal, temporal.Days(2)).Value(), expr.SubtractLocalInterval(nLocal, temporal.Days(2)).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractLocalIntervalValue(query.Value(oLocal), temporal.Days(2)), expr.SubtractLocalIntervalValue(expr.Select(nLocal), temporal.Days(2)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("SubtractLocalIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.SubtractLocalIntervalNullable(oMaybeLocal, temporal.Days(2)).Value(), expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.SubtractLocalIntervalNullable(oMaybeLocal, temporal.Days(2)).Value(), expr.SubtractLocalIntervalNullable(nMaybeLocal, temporal.Days(2)).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractLocalIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractLocalIntervalNullableValue(query.Value(oMaybeLocal), temporal.Days(2)), expr.SubtractLocalIntervalNullableValue(expr.Select(nMaybeLocal), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.SubtractLocalIntervalNullableValue(query.Value(oMaybeLocal), temporal.Days(2)), expr.SubtractLocalIntervalNullableValue(expr.Select(nMaybeLocal), temporal.Days(2)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("SubtractInstantInterval", func(t *testing.T) {
		scalarGolden(t, query.SubtractInstantInterval(oInstant, temporal.Days(2), query.UTCZone()).Value(), expr.SubtractInstantInterval(nInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractInstantIntervalValue(query.Value(oInstant), temporal.Days(2), query.UTCZone()), expr.SubtractInstantIntervalValue(expr.Select(nInstant), temporal.Days(2), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("SubtractInstantIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.SubtractInstantIntervalNullable(oMaybeInstant, temporal.Days(2), query.UTCZone()).Value(), expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.SubtractInstantIntervalNullable(oMaybeInstant, temporal.Days(2), query.UTCZone()).Value(), expr.SubtractInstantIntervalNullable(nMaybeInstant, temporal.Days(2), expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractInstantIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractInstantIntervalNullableValue(query.Value(oMaybeInstant), temporal.Days(2), query.UTCZone()), expr.SubtractInstantIntervalNullableValue(expr.Select(nMaybeInstant), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.SubtractInstantIntervalNullableValue(query.Value(oMaybeInstant), temporal.Days(2), query.UTCZone()), expr.SubtractInstantIntervalNullableValue(expr.Select(nMaybeInstant), temporal.Days(2), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SubtractClockElapsed", func(t *testing.T) {
		scalarGolden(t, query.SubtractClockElapsed(oClock, time.Second).Value(), expr.SubtractClockElapsed(nClock, time.Second).Value(), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractClockElapsedValue(query.Value(oClock), time.Second), expr.SubtractClockElapsedValue(expr.Select(nClock), time.Second), codec.WallTime(), "12:34:56.123456")
	})
	t.Run("SubtractClockElapsedNullable", func(t *testing.T) {
		scalarGolden(t, query.SubtractClockElapsedNullable(oMaybeClock, time.Second).Value(), expr.SubtractClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.SubtractClockElapsedNullable(oMaybeClock, time.Second).Value(), expr.SubtractClockElapsedNullable(nMaybeClock, time.Second).Value(), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("SubtractClockElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractClockElapsedNullableValue(query.Value(oMaybeClock), time.Second), expr.SubtractClockElapsedNullableValue(expr.Select(nMaybeClock), time.Second), codec.Nullable(codec.WallTime()), "12:34:56.123456")
		scalarGolden(t, query.SubtractClockElapsedNullableValue(query.Value(oMaybeClock), time.Second), expr.SubtractClockElapsedNullableValue(expr.Select(nMaybeClock), time.Second), codec.Nullable(codec.WallTime()), nil)
	})
	t.Run("AddIntervals", func(t *testing.T) {
		scalarGolden(t, query.AddIntervals(oPeriod, oPeriod).Value(), expr.AddIntervals(nPeriod, nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsValue", func(t *testing.T) {
		scalarGolden(t, query.AddIntervalsValue(query.Value(oPeriod), query.Value(oPeriod)), expr.AddIntervalsValue(expr.Select(nPeriod), expr.Select(nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("AddIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, query.AddIntervalsNullable(oMaybePeriod, oMaybePeriod).Value(), expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.AddIntervalsNullable(oMaybePeriod, oMaybePeriod).Value(), expr.AddIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("AddIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.AddIntervalsNullableValue(query.Value(oMaybePeriod), query.Value(oMaybePeriod)), expr.AddIntervalsNullableValue(expr.Select(nMaybePeriod), expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.AddIntervalsNullableValue(query.Value(oMaybePeriod), query.Value(oMaybePeriod)), expr.AddIntervalsNullableValue(expr.Select(nMaybePeriod), expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervals", func(t *testing.T) {
		scalarGolden(t, query.SubtractIntervals(oPeriod, oPeriod).Value(), expr.SubtractIntervals(nPeriod, nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractIntervalsValue(query.Value(oPeriod), query.Value(oPeriod)), expr.SubtractIntervalsValue(expr.Select(nPeriod), expr.Select(nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("SubtractIntervalsNullable", func(t *testing.T) {
		scalarGolden(t, query.SubtractIntervalsNullable(oMaybePeriod, oMaybePeriod).Value(), expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.SubtractIntervalsNullable(oMaybePeriod, oMaybePeriod).Value(), expr.SubtractIntervalsNullable(nMaybePeriod, nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("SubtractIntervalsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SubtractIntervalsNullableValue(query.Value(oMaybePeriod), query.Value(oMaybePeriod)), expr.SubtractIntervalsNullableValue(expr.Select(nMaybePeriod), expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.SubtractIntervalsNullableValue(query.Value(oMaybePeriod), query.Value(oMaybePeriod)), expr.SubtractIntervalsNullableValue(expr.Select(nMaybePeriod), expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateInterval", func(t *testing.T) {
		scalarGolden(t, query.NegateInterval(oPeriod).Value(), expr.NegateInterval(nPeriod).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalValue", func(t *testing.T) {
		scalarGolden(t, query.NegateIntervalValue(query.Value(oPeriod)), expr.NegateIntervalValue(expr.Select(nPeriod)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("NegateIntervalNullable", func(t *testing.T) {
		scalarGolden(t, query.NegateIntervalNullable(oMaybePeriod).Value(), expr.NegateIntervalNullable(nMaybePeriod).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.NegateIntervalNullable(oMaybePeriod).Value(), expr.NegateIntervalNullable(nMaybePeriod).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("NegateIntervalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.NegateIntervalNullableValue(query.Value(oMaybePeriod)), expr.NegateIntervalNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.NegateIntervalNullableValue(query.Value(oMaybePeriod)), expr.NegateIntervalNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifference", func(t *testing.T) {
		scalarGolden(t, query.LocalDifference(oLocal, oLocal).Value(), expr.LocalDifference(nLocal, nLocal).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceValue", func(t *testing.T) {
		scalarGolden(t, query.LocalDifferenceValue(query.Value(oLocal), query.Value(oLocal)), expr.LocalDifferenceValue(expr.Select(nLocal), expr.Select(nLocal)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("LocalDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, query.LocalDifferenceNullable(oMaybeLocal, oMaybeLocal).Value(), expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.LocalDifferenceNullable(oMaybeLocal, oMaybeLocal).Value(), expr.LocalDifferenceNullable(nMaybeLocal, nMaybeLocal).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("LocalDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.LocalDifferenceNullableValue(query.Value(oMaybeLocal), query.Value(oMaybeLocal)), expr.LocalDifferenceNullableValue(expr.Select(nMaybeLocal), expr.Select(nMaybeLocal)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.LocalDifferenceNullableValue(query.Value(oMaybeLocal), query.Value(oMaybeLocal)), expr.LocalDifferenceNullableValue(expr.Select(nMaybeLocal), expr.Select(nMaybeLocal)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifference", func(t *testing.T) {
		scalarGolden(t, query.InstantDifference(oInstant, oInstant).Value(), expr.InstantDifference(nInstant, nInstant).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceValue", func(t *testing.T) {
		scalarGolden(t, query.InstantDifferenceValue(query.Value(oInstant), query.Value(oInstant)), expr.InstantDifferenceValue(expr.Select(nInstant), expr.Select(nInstant)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("InstantDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, query.InstantDifferenceNullable(oMaybeInstant, oMaybeInstant).Value(), expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.InstantDifferenceNullable(oMaybeInstant, oMaybeInstant).Value(), expr.InstantDifferenceNullable(nMaybeInstant, nMaybeInstant).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("InstantDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.InstantDifferenceNullableValue(query.Value(oMaybeInstant), query.Value(oMaybeInstant)), expr.InstantDifferenceNullableValue(expr.Select(nMaybeInstant), expr.Select(nMaybeInstant)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.InstantDifferenceNullableValue(query.Value(oMaybeInstant), query.Value(oMaybeInstant)), expr.InstantDifferenceNullableValue(expr.Select(nMaybeInstant), expr.Select(nMaybeInstant)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifference", func(t *testing.T) {
		scalarGolden(t, query.ClockDifference(oClock, oClock).Value(), expr.ClockDifference(nClock, nClock).Value(), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceValue", func(t *testing.T) {
		scalarGolden(t, query.ClockDifferenceValue(query.Value(oClock), query.Value(oClock)), expr.ClockDifferenceValue(expr.Select(nClock), expr.Select(nClock)), codec.Interval(), "1 mon 2 days 03:04:05")
	})
	t.Run("ClockDifferenceNullable", func(t *testing.T) {
		scalarGolden(t, query.ClockDifferenceNullable(oMaybeClock, oMaybeClock).Value(), expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock).Value(), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.ClockDifferenceNullable(oMaybeClock, oMaybeClock).Value(), expr.ClockDifferenceNullable(nMaybeClock, nMaybeClock).Value(), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("ClockDifferenceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ClockDifferenceNullableValue(query.Value(oMaybeClock), query.Value(oMaybeClock)), expr.ClockDifferenceNullableValue(expr.Select(nMaybeClock), expr.Select(nMaybeClock)), codec.Nullable(codec.Interval()), "1 mon 2 days 03:04:05")
		scalarGolden(t, query.ClockDifferenceNullableValue(query.Value(oMaybeClock), query.Value(oMaybeClock)), expr.ClockDifferenceNullableValue(expr.Select(nMaybeClock), expr.Select(nMaybeClock)), codec.Nullable(codec.Interval()), nil)
	})
	t.Run("IntervalMonths", func(t *testing.T) {
		scalarGolden(t, query.IntervalMonths(oPeriod).Value(), expr.IntervalMonths(nPeriod).Value(), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalMonthsValue(query.Value(oPeriod)), expr.IntervalMonthsValue(expr.Select(nPeriod)), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalMonthsNullable", func(t *testing.T) {
		scalarGolden(t, query.IntervalMonthsNullable(oMaybePeriod).Value(), expr.IntervalMonthsNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, query.IntervalMonthsNullable(oMaybePeriod).Value(), expr.IntervalMonthsNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalMonthsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalMonthsNullableValue(query.Value(oMaybePeriod)), expr.IntervalMonthsNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, query.IntervalMonthsNullableValue(query.Value(oMaybePeriod)), expr.IntervalMonthsNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDays", func(t *testing.T) {
		scalarGolden(t, query.IntervalDays(oPeriod).Value(), expr.IntervalDays(nPeriod).Value(), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalDaysValue(query.Value(oPeriod)), expr.IntervalDaysValue(expr.Select(nPeriod)), codec.Signed[int32](), int64(3))
	})
	t.Run("IntervalDaysNullable", func(t *testing.T) {
		scalarGolden(t, query.IntervalDaysNullable(oMaybePeriod).Value(), expr.IntervalDaysNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, query.IntervalDaysNullable(oMaybePeriod).Value(), expr.IntervalDaysNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalDaysNullableValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalDaysNullableValue(query.Value(oMaybePeriod)), expr.IntervalDaysNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), int64(3))
		scalarGolden(t, query.IntervalDaysNullableValue(query.Value(oMaybePeriod)), expr.IntervalDaysNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[int32]()), nil)
	})
	t.Run("IntervalElapsed", func(t *testing.T) {
		scalarGolden(t, query.IntervalElapsed(oPeriod).Value(), expr.IntervalElapsed(nPeriod).Value(), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalElapsedValue(query.Value(oPeriod)), expr.IntervalElapsedValue(expr.Select(nPeriod)), codec.Signed[time.Duration](), int64(1000))
	})
	t.Run("IntervalElapsedNullable", func(t *testing.T) {
		scalarGolden(t, query.IntervalElapsedNullable(oMaybePeriod).Value(), expr.IntervalElapsedNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, query.IntervalElapsedNullable(oMaybePeriod).Value(), expr.IntervalElapsedNullable(nMaybePeriod).Value(), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("IntervalElapsedNullableValue", func(t *testing.T) {
		scalarGolden(t, query.IntervalElapsedNullableValue(query.Value(oMaybePeriod)), expr.IntervalElapsedNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[time.Duration]()), int64(1000))
		scalarGolden(t, query.IntervalElapsedNullableValue(query.Value(oMaybePeriod)), expr.IntervalElapsedNullableValue(expr.Select(nMaybePeriod)), codec.Nullable(codec.Signed[time.Duration]()), nil)
	})
	t.Run("ShiftDate", func(t *testing.T) {
		scalarGolden(t, query.ShiftDate(oDate, oPeriod).Value(), expr.ShiftDate(nDate, nPeriod).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftDateValue(query.Value(oDate), query.Value(oPeriod)), expr.ShiftDateValue(expr.Select(nDate), expr.Select(nPeriod)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftDateNullable", func(t *testing.T) {
		scalarGolden(t, query.ShiftDateNullable(oMaybeDate, oMaybePeriod).Value(), expr.ShiftDateNullable(nMaybeDate, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.ShiftDateNullable(oMaybeDate, oMaybePeriod).Value(), expr.ShiftDateNullable(nMaybeDate, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftDateNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftDateNullableValue(query.Value(oMaybeDate), query.Value(oMaybePeriod)), expr.ShiftDateNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.ShiftDateNullableValue(query.Value(oMaybeDate), query.Value(oMaybePeriod)), expr.ShiftDateNullableValue(expr.Select(nMaybeDate), expr.Select(nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocal", func(t *testing.T) {
		scalarGolden(t, query.ShiftLocal(oLocal, oPeriod).Value(), expr.ShiftLocal(nLocal, nPeriod).Value(), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftLocalValue(query.Value(oLocal), query.Value(oPeriod)), expr.ShiftLocalValue(expr.Select(nLocal), expr.Select(nPeriod)), codec.LocalDateTime(), "2024-02-29 12:34:56")
	})
	t.Run("ShiftLocalNullable", func(t *testing.T) {
		scalarGolden(t, query.ShiftLocalNullable(oMaybeLocal, oMaybePeriod).Value(), expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.ShiftLocalNullable(oMaybeLocal, oMaybePeriod).Value(), expr.ShiftLocalNullable(nMaybeLocal, nMaybePeriod).Value(), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftLocalNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftLocalNullableValue(query.Value(oMaybeLocal), query.Value(oMaybePeriod)), expr.ShiftLocalNullableValue(expr.Select(nMaybeLocal), expr.Select(nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), "2024-02-29 12:34:56")
		scalarGolden(t, query.ShiftLocalNullableValue(query.Value(oMaybeLocal), query.Value(oMaybePeriod)), expr.ShiftLocalNullableValue(expr.Select(nMaybeLocal), expr.Select(nMaybePeriod)), codec.Nullable(codec.LocalDateTime()), nil)
	})
	t.Run("ShiftInstant", func(t *testing.T) {
		scalarGolden(t, query.ShiftInstant(oInstant, oPeriod, query.UTCZone()).Value(), expr.ShiftInstant(nInstant, nPeriod, expr.UTCZone()).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftInstantValue(query.Value(oInstant), query.Value(oPeriod), query.UTCZone()), expr.ShiftInstantValue(expr.Select(nInstant), expr.Select(nPeriod), expr.UTCZone()), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	})
	t.Run("ShiftInstantNullable", func(t *testing.T) {
		scalarGolden(t, query.ShiftInstantNullable(oMaybeInstant, oMaybePeriod, query.UTCZone()).Value(), expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.ShiftInstantNullable(oMaybeInstant, oMaybePeriod, query.UTCZone()).Value(), expr.ShiftInstantNullable(nMaybeInstant, nMaybePeriod, expr.UTCZone()).Value(), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("ShiftInstantNullableValue", func(t *testing.T) {
		scalarGolden(t, query.ShiftInstantNullableValue(query.Value(oMaybeInstant), query.Value(oMaybePeriod), query.UTCZone()), expr.ShiftInstantNullableValue(expr.Select(nMaybeInstant), expr.Select(nMaybePeriod), expr.UTCZone()), codec.Nullable(codec.DateTime()), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
		scalarGolden(t, query.ShiftInstantNullableValue(query.Value(oMaybeInstant), query.Value(oMaybePeriod), query.UTCZone()), expr.ShiftInstantNullableValue(expr.Select(nMaybeInstant), expr.Select(nMaybePeriod), expr.UTCZone()), codec.Nullable(codec.DateTime()), nil)
	})
	t.Run("SphericalDistance", func(t *testing.T) {
		scalarGolden(t, query.SphericalDistance(oLatitude, oLatitude, query.GeographicPoint{}).Value(), expr.SphericalDistance(nLatitude, nLatitude, expr.GeographicPoint{}).Value(), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceValue", func(t *testing.T) {
		scalarGolden(t, query.SphericalDistanceValue(query.Value(oLatitude), query.Value(oLatitude), query.GeographicPoint{}), expr.SphericalDistanceValue(expr.Select(nLatitude), expr.Select(nLatitude), expr.GeographicPoint{}), codec.Float[float64](), float64(4.5))
	})
	t.Run("SphericalDistanceNullable", func(t *testing.T) {
		scalarGolden(t, query.SphericalDistanceNullable(oMaybeLatitude, oMaybeLatitude, query.GeographicPoint{}).Value(), expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{}).Value(), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, query.SphericalDistanceNullable(oMaybeLatitude, oMaybeLatitude, query.GeographicPoint{}).Value(), expr.SphericalDistanceNullable(nMaybeLatitude, nMaybeLatitude, expr.GeographicPoint{}).Value(), codec.Nullable(codec.Float[float64]()), nil)
	})
	t.Run("SphericalDistanceNullableValue", func(t *testing.T) {
		scalarGolden(t, query.SphericalDistanceNullableValue(query.Value(oMaybeLatitude), query.Value(oMaybeLatitude), query.GeographicPoint{}), expr.SphericalDistanceNullableValue(expr.Select(nMaybeLatitude), expr.Select(nMaybeLatitude), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), float64(4.5))
		scalarGolden(t, query.SphericalDistanceNullableValue(query.Value(oMaybeLatitude), query.Value(oMaybeLatitude), query.GeographicPoint{}), expr.SphericalDistanceNullableValue(expr.Select(nMaybeLatitude), expr.Select(nMaybeLatitude), expr.GeographicPoint{}), codec.Nullable(codec.Float[float64]()), nil)
	})
	scalarGolden(t, query.TransactionTime(scalarOldSource()).Value(), expr.TransactionTime(rowset.ModelScope(scalars.Records.Query())).Value(), codec.DateTime(), time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC))
	t.Run("TrimChars", func(t *testing.T) {
		scalarGolden(t, query.TrimChars(oName, oName).Value(), expr.TrimChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimCharsValue", func(t *testing.T) {
		scalarGolden(t, query.TrimCharsValue(query.Value(oName), query.Value(oName)), expr.TrimCharsValue(expr.Select(nName), expr.Select(nName)), codec.String[string](), "abc")
	})
	t.Run("TrimCharsNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimCharsNullable(oMaybeName, oMaybeName).Value(), expr.TrimCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimCharsNullableValue(query.Value(oMaybeName), query.Value(oMaybeName)), expr.TrimCharsNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftChars", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftChars(oName, oName).Value(), expr.TrimLeftChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsValue", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftCharsValue(query.Value(oName), query.Value(oName)), expr.TrimLeftCharsValue(expr.Select(nName), expr.Select(nName)), codec.String[string](), "abc")
	})
	t.Run("TrimLeftCharsNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftCharsNullable(oMaybeName, oMaybeName).Value(), expr.TrimLeftCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimLeftCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimLeftCharsNullableValue(query.Value(oMaybeName), query.Value(oMaybeName)), expr.TrimLeftCharsNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightChars", func(t *testing.T) {
		scalarGolden(t, query.TrimRightChars(oName, oName).Value(), expr.TrimRightChars(nName, nName).Value(), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsValue", func(t *testing.T) {
		scalarGolden(t, query.TrimRightCharsValue(query.Value(oName), query.Value(oName)), expr.TrimRightCharsValue(expr.Select(nName), expr.Select(nName)), codec.String[string](), "abc")
	})
	t.Run("TrimRightCharsNullable", func(t *testing.T) {
		scalarGolden(t, query.TrimRightCharsNullable(oMaybeName, oMaybeName).Value(), expr.TrimRightCharsNullable(nMaybeName, nMaybeName).Value(), codec.Nullable(codec.String[string]()), "abc")
	})
	t.Run("TrimRightCharsNullableValue", func(t *testing.T) {
		scalarGolden(t, query.TrimRightCharsNullableValue(query.Value(oMaybeName), query.Value(oMaybeName)), expr.TrimRightCharsNullableValue(expr.Select(nMaybeName), expr.Select(nMaybeName)), codec.Nullable(codec.String[string]()), "abc")
	})

}
