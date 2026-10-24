package datatable

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func oldReportFields() (query.OrderedField[reportRecord, int64], query.TextField[reportRecord, string], query.NullableTextField[reportRecord, string], query.OrderedField[reportRecord, decimal.Decimal]) {
	return query.NewOrderedField[reportRecord, int64]("report_rows", "id", codec.Signed[int64]()),
		query.NewTextField[reportRecord, string]("report_rows", "name", codec.String[string]()),
		query.NewNullableTextField[reportRecord, string]("report_rows", "note", codec.String[string]()),
		query.NewOrderedField[reportRecord, decimal.Decimal]("report_rows", "amount", codec.Decimal())
}

func oldReportSource(tenant int64) query.ProjectionQuery[reportRecord, ReportRow] {
	definition := query.Define("report_rows", "id", []query.Column{
		{Name: "id"}, {Name: "tenant_id"}, {Name: "name"}, {Name: "note", Nullable: true}, {Name: "amount"}, {Name: "deleted_at", Nullable: true},
	}, func(database.Row) (reportRecord, error) { panic("report must hydrate the projection only") },
		query.NewModelField("deleted_at", codec.Nullable(codec.DateTime()), func(row reportRecord) value.Nullable[temporal.DateTime] { return row.DeletedAt }),
	).WithSoftDeletes("deleted_at")
	owner := query.NewOrderedField[reportRecord, int64]("report_rows", "tenant_id", codec.Signed[int64]())
	base := query.ForModel(definition).Where(owner.Eq(tenant))
	id, name, note, amount := oldReportFields()
	outputID := query.NewProjectionField[ReportRow, int64]("id")
	outputName := query.NewProjectionField[ReportRow, string]("name")
	outputNote := query.NewProjectionField[ReportRow, value.Nullable[string]]("note")
	outputAmount := query.NewProjectionField[ReportRow, decimal.Decimal]("amount")
	projection := query.DefineProjection([]query.ProjectionColumn[ReportRow]{outputID.Column(), outputName.Column(), outputNote.Column(), outputAmount.Column()}, func(row database.Row) (ReportRow, error) {
		var result ReportRow
		err := row.Scan(codec.Signed[int64]().Scan(&result.ID), codec.String[string]().Scan(&result.Name), codec.Nullable(codec.String[string]()).Scan(&result.Note), codec.Decimal().Scan(&result.Amount))
		return result, err
	})
	return query.Project(base, projection, query.Map(outputID, query.Value(id)), query.Map(outputName, query.Value(name)), query.Map(outputNote, query.Value(note)), query.Map(outputAmount, query.Value(amount)))
}

func TestDatatableCutoverProjectionGolden(t *testing.T) {
	id, name, note, amount := oldReportFields()
	low, err := decimal.Parse("1.25")
	if err != nil {
		t.Fatal(err)
	}
	high, err := decimal.Parse("9.50")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		request    Request
		predicates []query.Predicate[reportRecord]
	}{
		{"scoped", Request{}, nil},
		{"search_literal", Request{Search: "A%_!"}, []query.Predicate[reportRecord]{query.Or(query.And(name.IContains("A%_!")))}},
		{"nullable_ne", Request{Filters: []Filter{{Op: NotEqual, Column: "note", Values: []string{"memo"}}}}, []query.Predicate[reportRecord]{query.Or(query.And(note.Ne("memo")), query.And(note.IsNull()))}},
		{"nullable_not", Request{Filters: []Filter{{Op: Not, Children: []Filter{{Op: Equal, Column: "note", Values: []string{"memo"}}}}}}, []query.Predicate[reportRecord]{query.Or(query.And(query.And(note.Eq("memo")).Not()), query.And(note.IsNull()))}},
		{"exact_between", Request{Filters: []Filter{{Op: Between, Column: "amount", Values: []string{"1.25", "9.50"}}}}, []query.Predicate[reportRecord]{amount.Gte(low), amount.Lte(high)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := Define(reportSpec())
			p, err := table.definition.prepare(tc.request, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := rows.Compile(source.rows)
			if err != nil {
				t.Fatal(err)
			}
			want, err := oldReportSource(7).Where(tc.predicates...).ReorderUnwindowed(name.Asc(), id.Asc()).Compile()
			if err != nil {
				t.Fatal(err)
			}
			if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
				t.Fatalf("projection SQL changed\nnew: %s %v\nold: %s %v", got.SQL(), got.Arguments(), want.SQL(), want.Arguments())
			}
		})
	}
}
