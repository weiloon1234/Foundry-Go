package datatable

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/codec"
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

type allKindsBindingSet struct {
	Name      validation.Binding[AllKindsRow, string]
	Count     validation.Binding[AllKindsRow, int64]
	CreatedAt validation.Binding[AllKindsRow, temporal.DateTime]
	Status    validation.Binding[AllKindsRow, enumSortStatus]
	Active    validation.Binding[AllKindsRow, bool]
	Nickname  validation.Binding[AllKindsRow, value.Nullable[string]]
	Discount  validation.Binding[AllKindsRow, value.Nullable[decimal.Decimal]]
	PaidAt    validation.Binding[AllKindsRow, value.Nullable[temporal.DateTime]]
	OptStatus validation.Binding[AllKindsRow, value.Nullable[enumSortStatus]]
}

func allKindsBindings() allKindsBindingSet {
	return allKindsBindingSet{
		Name:      validation.DefineBinding("name", func(r *AllKindsRow) *string { return &r.Name }),
		Count:     validation.DefineBinding("count", func(r *AllKindsRow) *int64 { return &r.Count }),
		CreatedAt: validation.DefineBinding("created_at", func(r *AllKindsRow) *temporal.DateTime { return &r.CreatedAt }),
		Status:    validation.DefineBinding("status", func(r *AllKindsRow) *enumSortStatus { return &r.Status }),
		Active:    validation.DefineBinding("active", func(r *AllKindsRow) *bool { return &r.Active }),
		Nickname:  validation.DefineBinding("nickname", func(r *AllKindsRow) *value.Nullable[string] { return &r.Nickname }),
		Discount:  validation.DefineBinding("discount", func(r *AllKindsRow) *value.Nullable[decimal.Decimal] { return &r.Discount }),
		PaidAt:    validation.DefineBinding("paid_at", func(r *AllKindsRow) *value.Nullable[temporal.DateTime] { return &r.PaidAt }),
		OptStatus: validation.DefineBinding("opt_status", func(r *AllKindsRow) *value.Nullable[enumSortStatus] { return &r.OptStatus }),
	}
}

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

func allKindsBase(context.Context, struct{}) (rows.SelectionSource[allKindsRecord], error) {
	return rows.From(allKindsModels.Query()), nil
}

// allKindsSource is the reference query: the same row selected through
// rows.Define + rows.Select.
func allKindsSource() rows.Query[allKindsRecord, AllKindsRow] {
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
	)
}

// TestDirectColumnConstructorsMatchColumn verifies all 9 direct constructors
// produce the same validated description as the general Column form with the
// equivalent options, and SQL and arguments byte-identical to the reference
// rows.Define + rows.Select query filtered, searched and sorted the same way.
func TestDirectColumnConstructorsMatchColumn(t *testing.T) {
	f := allKindsModels
	b := allKindsBindings()
	strCodec := foundryhttp.StringQuery[string]()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())
	boolCodec := foundryhttp.BoolQuery[bool]()

	shortTable := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.all_kinds",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(b.Name, "fields.name", f.Name),
			Number(b.Count, "fields.count", f.Count, intCodec),
			Time(b.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec),
			Enum(b.Status, "fields.status", f.Status, enumCodec),
			Bool(b.Active, "fields.active", f.Active),
			NullableText(b.Nickname, "fields.nickname", f.Nickname),
			NullableNumber(b.Discount, "fields.discount", f.Discount, decCodec),
			NullableTime(b.PaidAt, "fields.paid_at", f.PaidAt, dtCodec),
			NullableEnum(b.OptStatus, "fields.opt_status", f.OptStatus, enumCodec),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
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

	generalTable := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.all_kinds",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Column(b.Name, "fields.name", expr.AsSelected(expr.Row(nameRow)), SortBy(expr.AsSelected(expr.Row(nameRow))), FilterBy(Where(strCodec, expr.TextFilter(nameRow))), Searchable(nameRow), ExportAs(expr.AsSelected(expr.Row(nameRow)), ScalarCell(strCodec))),
			Column(b.Count, "fields.count", expr.AsSelected(expr.Row(countRow)), SortBy(expr.AsSelected(expr.Row(countRow))), FilterBy(Where(intCodec, expr.OrderFilter(countRow))), ExportAs(expr.AsSelected(expr.Row(countRow)), ScalarCell(intCodec))),
			Column(b.CreatedAt, "fields.created_at", expr.AsSelected(expr.Row(createdRow)), SortBy(expr.AsSelected(expr.Row(createdRow))), FilterBy(Where(dtCodec, expr.OrderFilter(createdRow))), ExportAs(expr.AsSelected(expr.Row(createdRow)), ScalarCell(dtCodec))),
			Column(b.Status, "fields.status", expr.AsSelected(expr.Row(statusRow)), SortBy(expr.AsSelected(expr.Row(statusRow))), FilterBy(Where(enumCodec, expr.CompareFilter(statusRow))), ExportAs(expr.AsSelected(expr.Row(statusRow)), ScalarCell(enumCodec))),
			Column(b.Active, "fields.active", expr.AsSelected(expr.Row(activeRow)), SortBy(expr.AsSelected(expr.Row(activeRow))), FilterBy(Where(boolCodec, expr.CompareFilter(activeRow))), ExportAs(expr.AsSelected(expr.Row(activeRow)), ScalarCell(boolCodec))),
			Column(b.Nickname, "fields.nickname", expr.AsSelected(expr.Row(nickRow)), SortBy(expr.AsSelected(expr.Row(nickRow))), FilterBy(NullableWhere(strCodec, expr.NullableTextFilter(nickRow))), Searchable(nickRow), ExportAs(expr.AsSelected(expr.Row(nickRow)), NullableCell(strCodec))),
			Column(b.Discount, "fields.discount", expr.AsSelected(expr.Row(discountRow)), SortBy(expr.AsSelected(expr.Row(discountRow))), FilterBy(NullableWhere(decCodec, expr.NullableOrderFilter(discountRow))), ExportAs(expr.AsSelected(expr.Row(discountRow)), NullableCell(decCodec))),
			Column(b.PaidAt, "fields.paid_at", expr.AsSelected(expr.Row(paidRow)), SortBy(expr.AsSelected(expr.Row(paidRow))), FilterBy(NullableWhere(dtCodec, expr.NullableOrderFilter(paidRow))), ExportAs(expr.AsSelected(expr.Row(paidRow)), NullableCell(dtCodec))),
			Column(b.OptStatus, "fields.opt_status", expr.AsSelected(expr.Row(optStatusRow)), SortBy(expr.AsSelected(expr.Row(optStatusRow))), FilterBy(NullableWhere(enumCodec, expr.NullableCompareFilter(optStatusRow))), ExportAs(expr.AsSelected(expr.Row(optStatusRow)), NullableCell(enumCodec))),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})

	shortDesc, err := shortTable.Description()
	if err != nil {
		t.Fatalf("shortTable.Description: %v", err)
	}
	generalDesc, err := generalTable.Description()
	if err != nil {
		t.Fatalf("generalTable.Description: %v", err)
	}
	if !reflect.DeepEqual(shortDesc, generalDesc) {
		t.Fatalf("direct constructors differ from Column:\nshort:   %+v\ngeneral: %+v", shortDesc.Columns, generalDesc.Columns)
	}

	// Compiled SQL and arguments match the reference query byte for byte.
	req := Request{
		Search:  "Ada",
		Sort:    []Sort{{Column: "created_at", Direction: Descending}},
		Filters: []Filter{{Column: "count", Op: Greater, Values: []string{"5"}}},
	}
	cfg := DefaultConfig()
	reference, err := generalTable.definition.prepare(req, cfg)
	if err != nil {
		t.Fatalf("reference prepare: %v", err)
	}
	filtered := rows.Having(allKindsSource().Where(reference.condition.where...), reference.condition.having...)
	want, err := rows.CompileRead(rows.Read(rows.ReorderUnwindowed(filtered, reference.orders...)))
	if err != nil {
		t.Fatalf("reference CompileRead: %v", err)
	}
	for label, table := range map[string]Table[allKindsRecord, AllKindsRow, struct{}]{"direct": shortTable, "general": generalTable} {
		prepared, err := table.definition.prepare(req, cfg)
		if err != nil {
			t.Fatalf("%s prepare: %v", label, err)
		}
		scoped, err := table.definition.scoped(t.Context(), struct{}{}, QueryAction, prepared)
		if err != nil {
			t.Fatalf("%s scoped: %v", label, err)
		}
		got, err := rows.CompileRead(rows.Read(scoped.rows))
		if err != nil {
			t.Fatalf("%s CompileRead: %v", label, err)
		}
		if got.SQL() != want.SQL() {
			t.Fatalf("%s SQL mismatch:\ngot:  %s\nwant: %s", label, got.SQL(), want.SQL())
		}
		if !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
			t.Fatalf("%s arguments mismatch:\ngot:  %#v\nwant: %#v", label, got.Arguments(), want.Arguments())
		}
	}
}

// TestColumnFeatureOptOuts verifies NoSort, NoFilter, NoSearch, and NoExport
// disable the corresponding capabilities cleanly.
func TestColumnFeatureOptOuts(t *testing.T) {
	f := allKindsModels
	b := allKindsBindings()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	table := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.feature_flags",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(b.Name, "fields.name", f.Name, NoSearch),
			NullableText(b.Nickname, "fields.nickname", f.Nickname, NoFilter),
			Number(b.Count, "fields.count", f.Count, intCodec, NoSort),
			Time(b.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec, NoExport),
			Enum(b.Status, "fields.status", f.Status, enumCodec, NoSort, NoFilter),
			Bool(b.Active, "fields.active", f.Active, NoExport, NoFilter),
			Hidden(b.Discount, f.Discount),
			Hidden(b.PaidAt, f.PaidAt),
			Hidden(b.OptStatus, f.OptStatus),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	desc, err := table.Description()
	if err != nil {
		t.Fatalf("Description: %v", err)
	}
	if len(desc.Columns) != 6 {
		t.Fatalf("expected 6 displayed columns (3 hidden), got %d", len(desc.Columns))
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
	b := allKindsBindings()
	intCodec := foundryhttp.IntegerQuery[int64]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	makeTable := func(col ColumnRegistration[allKindsRecord, AllKindsRow]) Table[allKindsRecord, AllKindsRow, struct{}] {
		return Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
			ID:        "reports.feature_validation",
			Row:       allKindsJSON(),
			Exports:   true,
			Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{col},
			Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
			Authorize: func(context.Context, struct{}, Action) error { return nil },
			Source:    allKindsBase,
		})
	}

	invalidCases := []struct {
		name string
		col  ColumnRegistration[allKindsRecord, AllKindsRow]
	}{
		{"zero_on_text", Text(b.Name, "fields.name", f.Name, ColumnFeature(0))},
		{"zero_on_number", Number(b.Count, "fields.count", f.Count, intCodec, ColumnFeature(0))},
		{"unknown_on_text", Text(b.Name, "fields.name", f.Name, ColumnFeature(99))},
		{"unknown_on_nullable_text", NullableText(b.Nickname, "fields.nickname", f.Nickname, ColumnFeature(99))},
		{"nosearch_on_number", Number(b.Count, "fields.count", f.Count, intCodec, NoSearch)},
		{"nosearch_on_time", Time(b.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec, NoSearch)},
		{"nosearch_on_enum", Enum(b.Status, "fields.status", f.Status, enumCodec, NoSearch)},
		{"nosearch_on_bool", Bool(b.Active, "fields.active", f.Active, NoSearch)},
		{"nosearch_on_nullable_number", NullableNumber(b.Discount, "fields.discount", f.Discount, decCodec, NoSearch)},
		{"nosearch_on_nullable_time", NullableTime(b.PaidAt, "fields.paid_at", f.PaidAt, dtCodec, NoSearch)},
		{"nosearch_on_nullable_enum", NullableEnum(b.OptStatus, "fields.opt_status", f.OptStatus, enumCodec, NoSearch)},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			// Displayed column errors precede the plan's unbound-field errors.
			if err := makeTable(tc.col).Validate(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "column feature") {
				t.Fatalf("%s: expected the column feature error, got %v", tc.name, err)
			}
		})
	}

	// Repeated valid flags remain harmless on both text and non-text constructors.
	repeated := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:      "reports.feature_repeats",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Text(b.Name, "fields.name", f.Name, NoSearch, NoSearch, NoSort, NoSort),
			Number(b.Count, "fields.count", f.Count, intCodec, NoSort, NoSort, NoFilter, NoFilter),
			Hidden(b.CreatedAt, f.CreatedAt),
			Hidden(b.Status, f.Status),
			Hidden(b.Active, f.Active),
			Hidden(b.Nickname, f.Nickname),
			Hidden(b.Discount, f.Discount),
			Hidden(b.PaidAt, f.PaidAt),
			Hidden(b.OptStatus, f.OptStatus),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := repeated.Validate(); err != nil {
		t.Fatalf("repeated valid ColumnFeature flags rejected: %v", err)
	}
}

// TestBoundColumnsDynamicAndValidation covers DynamicColumn + Dynamic,
// HiddenSelected (including ScanWith), slot binding and declaration error order.
func TestBoundColumnsDynamicAndValidation(t *testing.T) {
	f := allKindsModels
	b := allKindsBindings()
	nameSlot := NewDynamicSlot[allKindsRecord, string]()
	dtCodec := foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]()
	decCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	enumCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	type localeAuth struct{ locale string }

	table := Define(Spec[allKindsRecord, AllKindsRow, localeAuth]{
		ID:      "reports.dynamic_test",
		Row:     allKindsJSON(),
		Exports: true,
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			DynamicColumn(b.Name, "fields.name", nameSlot, SortBy(expr.AsSelected(expr.Row(expr.Value(f.Name)))), ExportAs(expr.AsSelected(expr.Row(expr.Value(f.Name))), ScalarCell(foundryhttp.StringQuery[string]()))),
			Column(b.Count, "fields.count", expr.AsSelected(expr.Row(expr.Value(f.Count))), SortBy(expr.AsSelected(expr.Row(expr.Value(f.Count)))), ScanWith[allKindsRecord](orm.Codec(codec.Signed[int64]()))),
			Time(b.CreatedAt, "fields.created_at", f.CreatedAt, dtCodec),
			Enum(b.Status, "fields.status", f.Status, enumCodec),
			Bool(b.Active, "fields.active", f.Active),
			HiddenSelected(b.Nickname, expr.AsSelected(expr.Row(expr.Value(f.Nickname))), ScanWith[allKindsRecord](orm.Codec(codec.Nullable(codec.String[string]())))),
			NullableNumber(b.Discount, "fields.discount", f.Discount, decCodec),
			NullableTime(b.PaidAt, "fields.paid_at", f.PaidAt, dtCodec),
			NullableEnum(b.OptStatus, "fields.opt_status", f.OptStatus, enumCodec),
		},
		Dynamic: []DynamicBinding[allKindsRecord, localeAuth]{
			BindDynamic(nameSlot, func(_ context.Context, a localeAuth) expr.Selected[allKindsRecord, string] {
				return expr.AsSelected(expr.Constant[allKindsRecord](codec.String[string](), a.locale))
			}),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, localeAuth, Action) error { return nil },
		Source: func(context.Context, localeAuth) (rows.SelectionSource[allKindsRecord], error) {
			return rows.From(f.Query()), nil
		},
	})
	if err := table.Validate(); err != nil {
		t.Fatalf("dynamic table Validate: %v", err)
	}
	prep, err := table.definition.prepare(Request{}, DefaultConfig())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	s1, err := table.definition.scoped(t.Context(), localeAuth{locale: "en"}, QueryAction, prep)
	if err != nil {
		t.Fatalf("scoped(en): %v", err)
	}
	s2, err := table.definition.scoped(t.Context(), localeAuth{locale: "ms"}, QueryAction, prep)
	if err != nil {
		t.Fatalf("scoped(ms): %v", err)
	}
	stmt1, err := rows.CompileRead(rows.Read(s1.rows))
	if err != nil {
		t.Fatalf("stmt1: %v", err)
	}
	stmt2, err := rows.CompileRead(rows.Read(s2.rows))
	if err != nil {
		t.Fatalf("stmt2: %v", err)
	}
	if reflect.DeepEqual(stmt1.Arguments(), stmt2.Arguments()) {
		t.Fatalf("expected dynamic selection per request, got identical arguments %#v", stmt1.Arguments())
	}

	// Unbound dynamic slot fails Define().Validate().
	unbound := Define(Spec[allKindsRecord, AllKindsRow, localeAuth]{
		ID:        "reports.unbound",
		Row:       allKindsJSON(),
		Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{DynamicColumn(b.Name, "fields.name", nameSlot)},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, localeAuth, Action) error { return nil },
		Source: func(context.Context, localeAuth) (rows.SelectionSource[allKindsRecord], error) {
			return rows.From(f.Query()), nil
		},
	})
	if err := unbound.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for unbound dynamic slot, got %v", err)
	}

	// Missing hidden row field fails Define().Validate().
	incomplete := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:        "reports.incomplete",
		Row:       allKindsJSON(),
		Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{Text(b.Name, "fields.name", f.Name)},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := incomplete.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for missing row fields, got %v", err)
	}

	// HiddenSelected rejects display-only options like SortBy.
	badHiddenOpt := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:        "reports.bad_hidden_opt",
		Row:       allKindsJSON(),
		Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{HiddenSelected(b.Name, expr.AsSelected(expr.Row(expr.Value(f.Name))), SortBy(expr.AsSelected(expr.Row(expr.Value(f.Name)))))},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := badHiddenOpt.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for SortBy on HiddenSelected, got %v", err)
	}

	// A zero validation.Binding reports "row field binding is not defined", not a
	// later plan error for the unbound field.
	var zeroBinding validation.Binding[AllKindsRow, string]
	missingBinding := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:        "reports.missing_binding",
		Row:       allKindsJSON(),
		Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{Text(zeroBinding, "fields.name", f.Name)},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := missingBinding.Validate(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "row field binding is not defined") {
		t.Fatalf("expected 'row field binding is not defined', got %v", err)
	}
	missingHiddenBinding := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:  "reports.missing_hidden_binding",
		Row: allKindsJSON(),
		Columns: []ColumnRegistration[allKindsRecord, AllKindsRow]{
			Hidden(zeroBinding, f.Name),
			Number(b.Count, "fields.count", f.Count, intCodecForTest()),
		},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := missingHiddenBinding.Validate(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "row field binding is not defined") {
		t.Fatalf("expected hidden 'row field binding is not defined', got %v", err)
	}

	// MaxColumns bounds displayed columns; a table must display at least one.
	hiddenOnly := Define(Spec[allKindsRecord, AllKindsRow, struct{}]{
		ID:        "reports.hidden_only",
		Row:       allKindsJSON(),
		Columns:   []ColumnRegistration[allKindsRecord, AllKindsRow]{Hidden(b.Name, f.Name)},
		Stable:    []expr.ProjectionOrder[allKindsRecord]{expr.Asc(expr.Value(f.ID))},
		Authorize: func(context.Context, struct{}, Action) error { return nil },
		Source:    allKindsBase,
	})
	if err := hiddenOnly.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatalf("expected fault.Invalid for a table without displayed columns, got %v", err)
	}
}

func intCodecForTest() foundryhttp.QueryCodec[int64] { return foundryhttp.IntegerQuery[int64]() }
