package datatable

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type allKindsRecord struct {
	ID        int64
	Name      string
	Count     int64
	CreatedAt temporal.DateTime
	Status    enumSortStatus
	Active    bool
	Nickname  value.Nullable[string]
	Discount  value.Nullable[decimal.Decimal]
	PaidAt    value.Nullable[temporal.DateTime]
	OptStatus value.Nullable[enumSortStatus]
}

type allKindsColumns struct {
	orm.Table[allKindsRecord, int64]
	ID        orm.Primary[allKindsRecord, int64]
	Name      orm.Text[allKindsRecord, string]
	Count     orm.Number[allKindsRecord, int64]
	CreatedAt orm.Time[allKindsRecord, temporal.DateTime]
	Status    orm.Enum[allKindsRecord, enumSortStatus]
	Active    orm.Column[allKindsRecord, bool]
	Nickname  orm.NullableText[allKindsRecord, string]
	Discount  orm.NullableNumber[allKindsRecord, decimal.Decimal]
	PaidAt    orm.NullableTime[allKindsRecord, temporal.DateTime]
	OptStatus orm.NullableEnum[allKindsRecord, enumSortStatus]
}

var allKindsModels = orm.Define("all_kinds_records", func(b orm.Builder[allKindsRecord], r *allKindsRecord) allKindsColumns {
	tbl := orm.TableOf(b, &r.ID)
	return allKindsColumns{
		Table:     tbl,
		ID:        orm.PrimaryOf(b, tbl.Key()),
		Name:      orm.TextOf(b, &r.Name),
		Count:     orm.NumberOf(b, &r.Count),
		CreatedAt: orm.TimeOf(b, &r.CreatedAt),
		Status:    orm.EnumOf(b, &r.Status),
		Active:    orm.BoolOf(b, &r.Active),
		Nickname:  orm.NullableTextOf(b, &r.Nickname),
		Discount:  orm.NullableNumberOf(b, &r.Discount),
		PaidAt:    orm.NullableTimeOf(b, &r.PaidAt),
		OptStatus: orm.NullableEnumOf(b, &r.OptStatus),
	}
})

type AllKindsRow struct {
	Name      string                            `json:"name"`
	Count     int64                             `json:"count"`
	CreatedAt temporal.DateTime                 `json:"created_at"`
	Status    enumSortStatus                    `json:"status"`
	Active    bool                              `json:"active"`
	Nickname  value.Nullable[string]            `json:"nickname"`
	Discount  value.Nullable[decimal.Decimal]   `json:"discount"`
	PaidAt    value.Nullable[temporal.DateTime] `json:"paid_at"`
	OptStatus value.Nullable[enumSortStatus]    `json:"opt_status"`
}

type allKindsRowFields struct {
	rows.Row[AllKindsRow]
	Name      rows.Field[AllKindsRow, string]
	Count     rows.Field[AllKindsRow, int64]
	CreatedAt rows.Field[AllKindsRow, temporal.DateTime]
	Status    rows.Field[AllKindsRow, enumSortStatus]
	Active    rows.Field[AllKindsRow, bool]
	Nickname  rows.Field[AllKindsRow, value.Nullable[string]]
	Discount  rows.Field[AllKindsRow, value.Nullable[decimal.Decimal]]
	PaidAt    rows.Field[AllKindsRow, value.Nullable[temporal.DateTime]]
	OptStatus rows.Field[AllKindsRow, value.Nullable[enumSortStatus]]
}

var allKindsRows = rows.Define(func(b rows.Builder[AllKindsRow], r *AllKindsRow) allKindsRowFields {
	return allKindsRowFields{
		Row:       b.Row(),
		Name:      rows.TextOf(b, &r.Name),
		Count:     rows.NumberOf(b, &r.Count),
		CreatedAt: rows.TimeOf(b, &r.CreatedAt),
		Status:    rows.EnumOf(b, &r.Status),
		Active:    rows.BoolOf(b, &r.Active),
		Nickname:  rows.NullableTextOf(b, &r.Nickname),
		Discount:  rows.NullableNumberOf(b, &r.Discount),
		PaidAt:    rows.NullableTimeOf(b, &r.PaidAt),
		OptStatus: rows.NullableEnumOf(b, &r.OptStatus),
	}
})

func allKindsJSON() contract.JSON[AllKindsRow] {
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.AllKindsRow"
	return contract.DefineJSON[AllKindsRow](contract.Schema{
		Root: root,
		Types: []contract.Type{
			{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "name", Type: "text", Required: true},
				{Name: "count", Type: "integer", Required: true},
				{Name: "created_at", Type: "datetime", Required: true},
				{Name: "status", Type: "status_enum", Required: true},
				{Name: "active", Type: "boolean", Required: true},
				{Name: "nickname", Type: "nullable_text", Required: true},
				{Name: "discount", Type: "nullable_decimal", Required: true},
				{Name: "paid_at", Type: "nullable_datetime", Required: true},
				{Name: "opt_status", Type: "nullable_status_enum", Required: true},
			}},
			{ID: "text", Kind: contract.StringKind},
			{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
			{ID: "datetime", Kind: contract.StringKind, Format: contract.DateTimeFormat},
			contract.EnumType("status_enum", enumSortActive.EnumDescriptor()),
			{ID: "boolean", Kind: contract.BooleanKind},
			{ID: "nullable_text", Kind: contract.AliasKind, Element: "text", Nullable: true},
			{ID: "decimal", Kind: contract.StringKind, Format: contract.DecimalFormat},
			{ID: "nullable_decimal", Kind: contract.AliasKind, Element: "decimal", Nullable: true},
			{ID: "nullable_datetime", Kind: contract.AliasKind, Element: "datetime", Nullable: true},
			{ID: "nullable_status_enum", Kind: contract.AliasKind, Element: "status_enum", Nullable: true},
		},
	})
}

func allKindsSource(context.Context, struct{}) (rows.Query[allKindsRecord, AllKindsRow], error) {
	f := allKindsModels
	return rows.Select(
		rows.From(f.Query()),
		allKindsRows,
		rows.Map(allKindsRows.Name, expr.Value(f.Name)),
		rows.Map(allKindsRows.Count, expr.Value(f.Count)),
		rows.Map(allKindsRows.CreatedAt, expr.Value(f.CreatedAt)),
		rows.Map(allKindsRows.Status, expr.Value(f.Status)),
		rows.Map(allKindsRows.Active, expr.Value(f.Active)),
		rows.Map(allKindsRows.Nickname, expr.Value(f.Nickname)),
		rows.Map(allKindsRows.Discount, expr.Value(f.Discount)),
		rows.Map(allKindsRows.PaidAt, expr.Value(f.PaidAt)),
		rows.Map(allKindsRows.OptStatus, expr.Value(f.OptStatus)),
	), nil
}

// TestDirectColumnConstructorsMatchDefineColumn verifies all 9 shipping
// Spec.Columns short-name constructors produce identical validated Table
// descriptions to manual DefineColumn wiring.
func TestDirectColumnConstructorsMatchDefineColumn(t *testing.T) {
	f := allKindsModels
	strCodec := foundryhttp.StringQuery[string]()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())
	boolCodec := foundryhttp.BoolQuery[bool]()

	fName := validation.DefineField("name", func(r AllKindsRow) string { return r.Name })
	fCount := validation.DefineField("count", func(r AllKindsRow) int64 { return r.Count })
	fCreatedAt := validation.DefineField("created_at", func(r AllKindsRow) temporal.DateTime { return r.CreatedAt })
	fStatus := validation.DefineField("status", func(r AllKindsRow) enumSortStatus { return r.Status })
	fActive := validation.DefineField("active", func(r AllKindsRow) bool { return r.Active })
	fNickname := validation.DefineField("nickname", func(r AllKindsRow) value.Nullable[string] { return r.Nickname })
	fDiscount := validation.DefineField("discount", func(r AllKindsRow) value.Nullable[decimal.Decimal] { return r.Discount })
	fPaidAt := validation.DefineField("paid_at", func(r AllKindsRow) value.Nullable[temporal.DateTime] { return r.PaidAt })
	fOptStatus := validation.DefineField("opt_status", func(r AllKindsRow) value.Nullable[enumSortStatus] { return r.OptStatus })

	shortTable := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.all_kinds",
		Row:     allKindsJSON(),
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

	nameRow := expr.TextRow(f.Name)
	countRow := expr.OrderedColumn(f.Count)
	createdRow := expr.OrderedColumn(f.CreatedAt)
	statusRow := expr.CompareColumn(f.Status)
	activeRow := expr.CompareColumn(f.Active)
	nickRow := expr.TextNullableRow(f.Nickname)
	discountRow := expr.NullableOrderedColumn(f.Discount)
	paidRow := expr.NullableOrderedColumn(f.PaidAt)
	optStatusRow := expr.NullableCompareColumn(f.OptStatus)

	manualTable := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.all_kinds",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			DefineColumn[allKindsRecord](fName, "fields.name", SortBy(expr.AsSelected(expr.Row(nameRow))), FilterBy(Where(strCodec, expr.TextFilter(nameRow))), Searchable(nameRow), ExportAs(expr.AsSelected(expr.Row(nameRow)), ScalarCell(strCodec))),
			DefineColumn[allKindsRecord](fCount, "fields.count", SortBy(expr.AsSelected(expr.Row(countRow))), FilterBy(Where(intCodec, expr.OrderFilter(countRow))), ExportAs(expr.AsSelected(expr.Row(countRow)), ScalarCell(intCodec))),
			DefineColumn[allKindsRecord](fCreatedAt, "fields.created_at", SortBy(expr.AsSelected(expr.Row(createdRow))), FilterBy(Where(dtCodec, expr.OrderFilter(createdRow))), ExportAs(expr.AsSelected(expr.Row(createdRow)), ScalarCell(dtCodec))),
			DefineColumn[allKindsRecord](fStatus, "fields.status", SortBy(expr.AsSelected(expr.Row(statusRow))), FilterBy(Where(enumCodec, expr.CompareFilter(statusRow))), ExportAs(expr.AsSelected(expr.Row(statusRow)), ScalarCell(enumCodec))),
			DefineColumn[allKindsRecord](fActive, "fields.active", SortBy(expr.AsSelected(expr.Row(activeRow))), FilterBy(Where(boolCodec, expr.CompareFilter(activeRow))), ExportAs(expr.AsSelected(expr.Row(activeRow)), ScalarCell(boolCodec))),
			DefineColumn[allKindsRecord](fNickname, "fields.nickname", SortBy(expr.AsSelected(expr.Row(nickRow))), FilterBy(NullableWhere(strCodec, expr.NullableTextFilter(nickRow))), Searchable(nickRow), ExportAs(expr.AsSelected(expr.Row(nickRow)), NullableCell(strCodec))),
			DefineColumn[allKindsRecord](fDiscount, "fields.discount", SortBy(expr.AsSelected(expr.Row(discountRow))), FilterBy(NullableWhere(decCodec, expr.NullableOrderFilter(discountRow))), ExportAs(expr.AsSelected(expr.Row(discountRow)), NullableCell(decCodec))),
			DefineColumn[allKindsRecord](fPaidAt, "fields.paid_at", SortBy(expr.AsSelected(expr.Row(paidRow))), FilterBy(NullableWhere(dtCodec, expr.NullableOrderFilter(paidRow))), ExportAs(expr.AsSelected(expr.Row(paidRow)), NullableCell(dtCodec))),
			DefineColumn[allKindsRecord](fOptStatus, "fields.opt_status", SortBy(expr.AsSelected(expr.Row(optStatusRow))), FilterBy(NullableWhere(enumCodec, expr.NullableCompareFilter(optStatusRow))), ExportAs(expr.AsSelected(expr.Row(optStatusRow)), NullableCell(enumCodec))),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsSource,
	})

	shortDesc, err := shortTable.Description()
	if err != nil {
		t.Fatalf("shortTable.Description: %v", err)
	}
	manualDesc, err := manualTable.Description()
	if err != nil {
		t.Fatalf("manualTable.Description: %v", err)
	}
	if !reflect.DeepEqual(shortDesc, manualDesc) {
		t.Fatalf("short-name constructors differ from DefineColumn:\nshort:  %+v\nmanual: %+v", shortDesc.Columns, manualDesc.Columns)
	}
}

// TestColumnFeatureOptOuts verifies NoSort, NoFilter, NoSearch, and NoExport
// disable the corresponding capabilities cleanly.
func TestColumnFeatureOptOuts(t *testing.T) {
	f := allKindsModels
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	table := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.feature_flags",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(validation.DefineField("name", func(r AllKindsRow) string { return r.Name }), "fields.name", f.Name, NoSearch),
			NullableText(validation.DefineField("nickname", func(r AllKindsRow) value.Nullable[string] { return r.Nickname }), "fields.nickname", f.Nickname, NoFilter),
			Number(validation.DefineField("count", func(r AllKindsRow) int64 { return r.Count }), "fields.count", f.Count, intCodec, NoSort),
			Time(validation.DefineField("created_at", func(r AllKindsRow) temporal.DateTime { return r.CreatedAt }), "fields.created_at", f.CreatedAt, dtCodec, NoExport),
			Enum(validation.DefineField("status", func(r AllKindsRow) enumSortStatus { return r.Status }), "fields.status", f.Status, enumCodec, NoSort, NoFilter),
			Bool(validation.DefineField("active", func(r AllKindsRow) bool { return r.Active }), "fields.active", f.Active, NoExport, NoFilter),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsSource,
	})
	desc, err := table.Description()
	if err != nil {
		t.Fatalf("Description: %v", err)
	}
	byName := make(map[string]ColumnInfo, len(desc.Columns))
	for _, c := range desc.Columns {
		byName[c.Name] = c
	}

	if c := byName["name"]; !c.Sortable || c.Filter == nil || c.Searchable || !c.Exportable {
		t.Fatalf("name (NoSearch): unexpected flags %+v", c)
	}
	if c := byName["nickname"]; !c.Sortable || c.Filter != nil || c.Searchable || !c.Exportable {
		t.Fatalf("nickname (NoFilter): unexpected flags %+v", c)
	}
	if c := byName["count"]; c.Sortable || c.Filter == nil || !c.Exportable {
		t.Fatalf("count (NoSort): unexpected flags %+v", c)
	}
	if c := byName["created_at"]; !c.Sortable || c.Filter == nil || c.Exportable {
		t.Fatalf("created_at (NoExport): unexpected flags %+v", c)
	}
	if c := byName["status"]; c.Sortable || c.Filter != nil || !c.Exportable {
		t.Fatalf("status (NoSort, NoFilter): unexpected flags %+v", c)
	}
	if c := byName["active"]; !c.Sortable || c.Filter != nil || c.Exportable {
		t.Fatalf("active (NoExport, NoFilter): unexpected flags %+v", c)
	}
}

// TestColumnFeatureValidationRejectsInvalidValuesAndAllowsRepeats verifies that
// zero/unknown ColumnFeature values and NoSearch on non-text constructors fail
// declaration validation with fault.Invalid, while repeated valid flags remain
// harmless.
func TestColumnFeatureValidationRejectsInvalidValuesAndAllowsRepeats(t *testing.T) {
	f := allKindsModels
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

	makeTable := func(col ColumnRegistration[allKindsRecord, AllKindsRow]) Table[allKindsRecord, AllKindsRow, struct{}] {
		return Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
			ID:        "reports.feature_validation",
			Row:       allKindsJSON(),
			Exports:   true,
			Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{col},
			Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source:    allKindsSource,
		})
	}

	invalidCases := []struct {
		name string
		col  ColumnRegistration[allKindsRecord, AllKindsRow]
	}{
		{"zero_on_text", Text(fName, "fields.name", f.Name, ColumnFeature(0))},
		{"zero_on_number", Number(fCount, "fields.count", f.Count, intCodec, ColumnFeature(0))},
		{"unknown_on_text", Text(fName, "fields.name", f.Name, ColumnFeature(99))},
		{"unknown_on_nullable_text", NullableText(fNickname, "fields.nickname", f.Nickname, ColumnFeature(99))},
		{"nosearch_on_number", Number(fCount, "fields.count", f.Count, intCodec, NoSearch)},
		{"nosearch_on_time", Time(fCreatedAt, "fields.created_at", f.CreatedAt, dtCodec, NoSearch)},
		{"nosearch_on_enum", Enum(fStatus, "fields.status", f.Status, enumCodec, NoSearch)},
		{"nosearch_on_bool", Bool(fActive, "fields.active", f.Active, NoSearch)},
		{"nosearch_on_nullable_number", NullableNumber(fDiscount, "fields.discount", f.Discount, decCodec, NoSearch)},
		{"nosearch_on_nullable_time", NullableTime(fPaidAt, "fields.paid_at", f.PaidAt, dtCodec, NoSearch)},
		{"nosearch_on_nullable_enum", NullableEnum(fOptStatus, "fields.opt_status", f.OptStatus, enumCodec, NoSearch)},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := makeTable(tc.col).Validate(); !errors.Is(err, fault.Invalid) {
				t.Fatalf("%s: expected fault.Invalid, got %v", tc.name, err)
			}
		})
	}

	// Repeated valid flags remain harmless on both text and non-text constructors.
	repeated := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.feature_repeats",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(fName, "fields.name", f.Name, NoSearch, NoSearch, NoSort, NoSort),
			Number(fCount, "fields.count", f.Count, intCodec, NoSort, NoSort, NoFilter, NoFilter),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsSource,
	})
	if err := repeated.Validate(); err != nil {
		t.Fatalf("repeated valid ColumnFeature flags rejected: %v", err)
	}
}
