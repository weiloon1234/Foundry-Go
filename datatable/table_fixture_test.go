package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type reportRecord struct {
	ID        int64
	TenantID  int64
	Name      string
	Note      value.Nullable[string]
	Amount    decimal.Decimal
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

type reportModelColumns struct {
	orm.Table[reportRecord, int64]
	ID        orm.Primary[reportRecord, int64]
	TenantID  orm.Number[reportRecord, int64]
	Name      orm.Text[reportRecord, string]
	Note      orm.NullableText[reportRecord, string]
	Amount    orm.Number[reportRecord, decimal.Decimal]
	DeletedAt orm.SoftDelete[reportRecord, temporal.DateTime]
}

var reportModels = orm.Define("report_rows", func(b orm.Builder[reportRecord], r *reportRecord) reportModelColumns {
	table := orm.TableOf(b, &r.ID)
	return reportModelColumns{table, orm.PrimaryOf(b, table.Key()), orm.NumberOf(b, &r.TenantID), orm.TextOf(b, &r.Name), orm.NullableTextOf(b, &r.Note), orm.NumberOf(b, &r.Amount), orm.SoftDeleteOf(b, &r.DeletedAt)}
})

type reportRowColumns struct {
	rows.Row[ReportRow]
	ID     rows.Field[ReportRow, int64]
	Name   rows.Field[ReportRow, string]
	Note   rows.Field[ReportRow, value.Nullable[string]]
	Amount rows.Field[ReportRow, decimal.Decimal]
}

var reportRows = rows.Define(func(b rows.Builder[ReportRow], r *ReportRow) reportRowColumns {
	return reportRowColumns{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Name), rows.NullableTextOf(b, &r.Note), rows.NumberOf(b, &r.Amount)}
})

func reportFields() (expr.OrderedRowExpression[reportRecord, int64], expr.TextRowExpression[reportRecord, string], expr.NullableTextRowExpression[reportRecord, string], expr.OrderedRowExpression[reportRecord, decimal.Decimal]) {
	return expr.OrderRow(expr.Value(reportModels.ID)), expr.TextRow(reportModels.Name), expr.TextNullableRow(reportModels.Note), expr.OrderRow(expr.Value(reportModels.Amount))
}
func reportSource(tenant int64) rows.Query[reportRecord, ReportRow] {
	base := reportModels.Query().Where(reportModels.TenantID.Eq(tenant))
	return rows.Select(rows.From(base), reportRows, rows.Map(reportRows.ID, expr.Value(reportModels.ID)), rows.Map(reportRows.Name, expr.Value(reportModels.Name)), rows.Map(reportRows.Note, expr.Value(reportModels.Note)), rows.Map(reportRows.Amount, expr.Value(reportModels.Amount)))
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
		DefaultSort: []Sort{{Column: "name", Direction: Ascending}}, Stable: []expr.ProjectionOrder[reportRecord]{id.Value().Asc()},
		Authorize: func(_ context.Context, actor reportActor, _ Action) error {
			if !actor.Allowed {
				return auth.Forbidden
			}
			return nil
		},
		Source: func(_ context.Context, actor reportActor) (rows.Query[reportRecord, ReportRow], error) {
			return reportSource(actor.Tenant), nil
		},
	}
}
