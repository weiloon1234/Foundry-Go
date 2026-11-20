package datatable

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// BenchmarkDirectColumnSpecConstruction measures Spec.Columns construction
// across all 9 shipping short-name datatable constructors.
func BenchmarkDirectColumnSpecConstruction(b *testing.B) {
	f := allKindsModels
	rowJSON := allKindsJSON()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	fName := validation.DefineField("name", func(r AllKindsRow) string { return r.Name })
	fCount := validation.DefineField("count", func(r AllKindsRow) int64 { return r.Count })
	fCreatedAt := validation.DefineField("created_at", func(r AllKindsRow) temporal.DateTime { return r.CreatedAt })
	fStatus := validation.DefineField("status", func(r AllKindsRow) enumSortStatus { return r.Status })
	fActive := validation.DefineField("active", func(r AllKindsRow) bool { return r.Active })
	fNickname := validation.DefineField("nickname", func(r AllKindsRow) value.Nullable[string] { return r.Nickname })
	fDiscount := validation.DefineField("discount", func(r AllKindsRow) value.Nullable[decimal.Decimal] { return r.Discount })
	fPaidAt := validation.DefineField("paid_at", func(r AllKindsRow) value.Nullable[temporal.DateTime] { return r.PaidAt })
	fOptStatus := validation.DefineField("opt_status", func(r AllKindsRow) value.Nullable[enumSortStatus] { return r.OptStatus })

	b.ReportAllocs()
	for b.Loop() {
		table := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
			ID:      "reports.all_kinds",
			Row:     rowJSON,
			Exports: true,
			Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
				Text(fName, "fields.name", f.Name),
				Number(fCount, "fields.count", f.Count, intCodec),
				Time(fCreatedAt, "fields.created_at", f.CreatedAt, dtCodec),
				Enum(fStatus, "fields.status", f.Status, enumCodec),
				Bool(fActive, "fields.active", f.Active),
				NullableText(fNickname, "fields.nickname", f.Nickname),
				NullableNumber(fDiscount, "fields.discount", f.Discount, decCodec),
				NullableTime(fPaidAt, "fields.paid_at", f.PaidAt, dtCodec),
				NullableEnum(fOptStatus, "fields.opt_status", f.OptStatus, enumCodec),
			},
			Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source:    allKindsSource,
		})
		if err := table.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
