package datatable

import (
	"context"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type enumSortStatus string

const (
	enumSortActive   enumSortStatus = "active"
	enumSortDisabled enumSortStatus = "disabled"
)

func (s enumSortStatus) MarshalText() ([]byte, error) { return []byte(s), nil }
func (s *enumSortStatus) UnmarshalText(data []byte) error {
	*s = enumSortStatus(data)
	return nil
}

func (enumSortStatus) EnumDescriptor() enum.Descriptor[enumSortStatus] {
	return enum.Describe("github.com/weiloon1234/Foundry-Go/datatable", "EnumSortStatus",
		enum.Case[enumSortStatus]{Name: "Active", Value: enumSortActive},
		enum.Case[enumSortStatus]{Name: "Disabled", Value: enumSortDisabled},
	)
}

type enumSortRecord struct {
	ID        int64
	Status    enumSortStatus
	OptStatus value.Nullable[enumSortStatus]
}

type enumSortRecordFields struct {
	orm.Table[enumSortRecord, int64]
	Status    orm.Enum[enumSortRecord, enumSortStatus]
	OptStatus orm.NullableEnum[enumSortRecord, enumSortStatus]
}

var enumSortModels = orm.Define("enum_sort_records", func(b orm.Builder[enumSortRecord], r *enumSortRecord) enumSortRecordFields {
	table := orm.TableOf(b, &r.ID)
	return enumSortRecordFields{
		Table:     table,
		Status:    orm.EnumOf(b, &r.Status),
		OptStatus: orm.NullableEnumOf(b, &r.OptStatus),
	}
})

type EnumSortRow struct {
	ID        int64                          `json:"id,string"`
	Status    enumSortStatus                 `json:"status"`
	OptStatus value.Nullable[enumSortStatus] `json:"opt_status"`
}

func enumSortJSON() contract.JSON[EnumSortRow] {
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.EnumSortRow"
	const statusType contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.EnumSortStatus"
	const nullableStatusType contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.NullableEnumSortStatus"
	statusDesc := enumSortActive.EnumDescriptor()
	return contract.DefineJSON[EnumSortRow](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "id", Type: "quoted_id", Required: true},
			{Name: "status", Type: statusType, Required: true},
			{Name: "opt_status", Type: nullableStatusType, Required: true},
		}},
		{ID: "quoted_id", Kind: contract.QuotedKind, Element: "integer"},
		{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		contract.EnumType(statusType, statusDesc),
		{ID: nullableStatusType, Kind: contract.AliasKind, Element: statusType, Nullable: true},
	}})
}

type enumSortRowFields struct {
	rows.Row[EnumSortRow]
	ID        rows.Field[EnumSortRow, int64]
	Status    rows.Field[EnumSortRow, enumSortStatus]
	OptStatus rows.Field[EnumSortRow, value.Nullable[enumSortStatus]]
}

var enumSortRows = rows.Define(func(b rows.Builder[EnumSortRow], r *EnumSortRow) enumSortRowFields {
	return enumSortRowFields{
		Row:       b.Row(),
		ID:        rows.NumberOf(b, &r.ID),
		Status:    rows.EnumOf(b, &r.Status),
		OptStatus: rows.NullableEnumOf(b, &r.OptStatus),
	}
})

type enumSortActor struct {
	Allowed bool
}

func TestEnumRowSortAndShortStableSpelling(t *testing.T) {
	statusCmp := expr.CompareRow(expr.Value(enumSortModels.Status))
	optStatusCmp := expr.CompareNullableRow(expr.Value(enumSortModels.OptStatus))
	statusCodec := foundryhttp.EnumQuery[enumSortStatus, *enumSortStatus](enumSortActive.EnumDescriptor())

	spec := Spec[enumSortRecord, EnumSortRow, enumSortActor]{
		ID:      "test.enum_sort",
		Row:     enumSortJSON(),
		Exports: false,
		Columns: []ColumnRegistration[enumSortRecord, EnumSortRow]{
			DefineColumn[enumSortRecord](validation.DefineField("id", func(r EnumSortRow) int64 { return r.ID }), "test.id").Registration(),
			DefineColumn[enumSortRecord](validation.DefineField("status", func(r EnumSortRow) enumSortStatus { return r.Status }), "test.status").
				SortBy(statusCmp.Value()).
				FilterBy(Where(statusCodec, statusCmp)).
				ExportAs(ScalarCell(statusCodec)).
				Registration(),
			DefineColumn[enumSortRecord](validation.DefineField("opt_status", func(r EnumSortRow) value.Nullable[enumSortStatus] { return r.OptStatus }), "test.opt_status").
				SortBy(optStatusCmp.Value()).
				FilterBy(NullableWhere(statusCodec, optStatusCmp)).
				ExportAs(NullableCell(statusCodec)).
				Registration(),
		},
		Stable: []expr.ProjectionOrder[enumSortRecord]{expr.Value(enumSortModels.Key()).Asc()},
		Source: func(ctx context.Context, actor enumSortActor) (rows.Query[enumSortRecord, EnumSortRow], error) {
			return rows.Select(rows.From(enumSortModels.Query()), enumSortRows,
				rows.Map(enumSortRows.ID, expr.Value(enumSortModels.Key())),
				rows.Map(enumSortRows.Status, expr.Value(enumSortModels.Status)),
				rows.Map(enumSortRows.OptStatus, expr.Value(enumSortModels.OptStatus)),
			), nil
		},
		Authorize: func(ctx context.Context, actor enumSortActor, action Action) error {
			return nil
		},
	}

	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatalf("table validation failed: %v", err)
	}

	config := DefaultConfig()
	actor := enumSortActor{Allowed: true}

	cases := []struct {
		name    string
		sort    []Sort
		wantSQL string
	}{
		{
			name:    "enum asc with short stable",
			sort:    []Sort{{Column: "status", Direction: Ascending}},
			wantSQL: `ORDER BY "enum_sort_records"."status" ASC, "enum_sort_records"."id" ASC`,
		},
		{
			name:    "enum desc with short stable",
			sort:    []Sort{{Column: "status", Direction: Descending}},
			wantSQL: `ORDER BY "enum_sort_records"."status" DESC, "enum_sort_records"."id" ASC`,
		},
		{
			name:    "nullable enum asc with short stable",
			sort:    []Sort{{Column: "opt_status", Direction: Ascending}},
			wantSQL: `ORDER BY "enum_sort_records"."opt_status" ASC, "enum_sort_records"."id" ASC`,
		},
		{
			name:    "nullable enum desc with short stable",
			sort:    []Sort{{Column: "opt_status", Direction: Descending}},
			wantSQL: `ORDER BY "enum_sort_records"."opt_status" DESC, "enum_sort_records"."id" ASC`,
		},
		{
			name:    "default stable sort",
			sort:    nil,
			wantSQL: `ORDER BY "enum_sort_records"."id" ASC`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := table.definition.prepare(Request{Sort: tc.sort}, config)
			if err != nil {
				t.Fatalf("prepare failed: %v", err)
			}
			source, err := table.definition.scoped(t.Context(), actor, QueryAction, prepared)
			if err != nil {
				t.Fatalf("scoped failed: %v", err)
			}
			statement, err := rows.Compile(source.rows)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			if !strings.Contains(statement.SQL(), tc.wantSQL) {
				t.Fatalf("expected SQL containing %q, got: %s", tc.wantSQL, statement.SQL())
			}
		})
	}
}
