package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type reportRecord struct {
	DeletedAt value.Nullable[temporal.DateTime]
}
type reportActor struct {
	Tenant  int64
	Allowed bool
}
type ReportRow struct {
	ID     int64                  `json:"id,string"`
	Name   string                 `json:"name"`
	Note   value.Nullable[string] `json:"note"`
	Amount decimal.Decimal        `json:"amount"`
}

// Handwritten descriptors exercise the explicit declaration boundary here.
// The public consumer fixture exercises generation from the same Go field types.
func reportJSON() contract.JSON[ReportRow] {
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.ReportRow"
	return contract.DefineJSON[ReportRow](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "id", Type: "quoted_id", Required: true}, {Name: "name", Type: "text", Required: true},
			{Name: "note", Type: "nullable_text", Required: true}, {Name: "amount", Type: "decimal", Required: true},
		}},
		{ID: "quoted_id", Kind: contract.QuotedKind, Element: "integer"},
		{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		{ID: "text", Kind: contract.StringKind},
		{ID: "nullable_text", Kind: contract.AliasKind, Element: "text", Nullable: true},
		{ID: "decimal", Kind: contract.StringKind, Format: contract.DecimalFormat},
	}})
}

func reportFields() (query.OrderedField[reportRecord, int64], query.TextField[reportRecord, string], query.NullableTextField[reportRecord, string], query.OrderedField[reportRecord, decimal.Decimal]) {
	return query.NewOrderedField[reportRecord, int64]("report_rows", "id", codec.Signed[int64]()),
		query.NewTextField[reportRecord, string]("report_rows", "name", codec.String[string]()),
		query.NewNullableTextField[reportRecord, string]("report_rows", "note", codec.String[string]()),
		query.NewOrderedField[reportRecord, decimal.Decimal]("report_rows", "amount", codec.Decimal())
}

func reportSource(tenant int64) query.ProjectionQuery[reportRecord, ReportRow] {
	definition := query.Define("report_rows", "id", []query.Column{
		{Name: "id"}, {Name: "tenant_id"}, {Name: "name"}, {Name: "note", Nullable: true}, {Name: "amount"}, {Name: "deleted_at", Nullable: true},
	}, func(database.Row) (reportRecord, error) { panic("report must hydrate the projection only") },
		query.NewModelField("deleted_at", codec.Nullable(codec.DateTime()), func(row reportRecord) value.Nullable[temporal.DateTime] { return row.DeletedAt }),
	).WithSoftDeletes("deleted_at")
	owner := query.NewOrderedField[reportRecord, int64]("report_rows", "tenant_id", codec.Signed[int64]())
	base := query.ForModel(definition).Where(owner.Eq(tenant))
	id, name, note, amount := reportFields()
	outputID := query.NewProjectionField[ReportRow, int64]("id")
	outputName := query.NewProjectionField[ReportRow, string]("name")
	outputNote := query.NewProjectionField[ReportRow, value.Nullable[string]]("note")
	outputAmount := query.NewProjectionField[ReportRow, decimal.Decimal]("amount")
	projection := query.DefineProjection([]query.ProjectionColumn[ReportRow]{outputID.Column(), outputName.Column(), outputNote.Column(), outputAmount.Column()}, func(row database.Row) (ReportRow, error) {
		var result ReportRow
		err := row.Scan(codec.Signed[int64]().Scan(&result.ID), codec.String[string]().Scan(&result.Name), codec.Nullable(codec.String[string]()).Scan(&result.Note), codec.Decimal().Scan(&result.Amount))
		return result, err
	})
	return query.Project(base, projection, query.Map(outputID, id.Value()), query.Map(outputName, name.Value()), query.Map(outputNote, note.Value()), query.Map(outputAmount, amount.Value()))
}

func reportSpec() Spec[reportRecord, ReportRow, reportActor] {
	id, name, note, amount := reportFields()
	idCodec := foundryhttp.IntegerQuery[int64]()
	textCodec := foundryhttp.StringQuery[string]()
	amountCodec := foundryhttp.TextQuery[decimal.Decimal, *decimal.Decimal]()
	return Spec[reportRecord, ReportRow, reportActor]{
		ID: "reports.members", Row: reportJSON(), Exports: true,
		Columns: []ColumnRegistration[reportRecord, ReportRow]{
			DefineColumn[reportRecord](validation.DefineField("id", func(row ReportRow) int64 { return row.ID }), "reports.id").SortBy(id.Value()).FilterBy(Where(idCodec, id)).ExportAs(ScalarCell(idCodec)).Registration(),
			DefineColumn[reportRecord](validation.DefineField("name", func(row ReportRow) string { return row.Name }), "reports.name").SortBy(name.Value()).FilterBy(Where(textCodec, name)).Searchable().ExportAs(ScalarCell(textCodec)).Registration(),
			DefineColumn[reportRecord](validation.DefineField("note", func(row ReportRow) value.Nullable[string] { return row.Note }), "reports.note").SortBy(note.Value()).FilterBy(NullableWhere(textCodec, note)).ExportAs(NullableCell(textCodec)).Registration(),
			DefineColumn[reportRecord](validation.DefineField("amount", func(row ReportRow) decimal.Decimal { return row.Amount }), "reports.amount").SortBy(amount.Value()).FilterBy(Where(amountCodec, amount)).ExportAs(ScalarCell(amountCodec)).Registration(),
		},
		DefaultSort: []Sort{{Column: "name", Direction: Ascending}}, Stable: []query.ProjectionOrder[reportRecord]{id.Asc()},
		Authorize: func(_ context.Context, actor reportActor, _ Action) error {
			if !actor.Allowed {
				return auth.Forbidden
			}
			return nil
		},
		Source: func(_ context.Context, actor reportActor) (query.ProjectionQuery[reportRecord, ReportRow], error) {
			return reportSource(actor.Tenant), nil
		},
	}
}
