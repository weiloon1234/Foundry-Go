package datatable

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type computedNoteSide struct{}

type correlatedNoteFilter struct {
	expr.NullableTextFilterAdapter[reportRecord, string]
	text expr.NullableTextRowExpression[reportRecord, string]
}

func (f correlatedNoteFilter) IContains(v string) expr.Predicate[reportRecord] {
	return expr.NullableTextIContains(f.text, v)
}
func (f correlatedNoteFilter) Like(v string) expr.Predicate[reportRecord] {
	return expr.NullableTextLike(f.text, v)
}

func TestPostgresDatatableComputedNullableTextFilter(t *testing.T) {
	fixture := reportPostgres(t)
	outer := rows.From(reportModels.Query())
	innerQuery := rows.As[computedNoteSide](reportModels.Query(), "computed_note")
	correlation := rows.Correlate(outer, innerQuery)
	inner := rows.InnerScope(correlation, rows.Scope(innerQuery))
	parent := rows.OuterScope(correlation, rows.ModelScope(reportModels.Query()))
	scalar := rows.SelectCorrelatedScalar(correlation, expr.AsSelected(expr.Row(expr.At(inner, orm.NullableTextExpr(reportModels.Note))))).
		Where(expr.Equal(expr.At(inner, orm.KeyExpr(reportModels.ID)), expr.At(parent, orm.KeyExpr(reportModels.ID)))).Limit(1)
	text := expr.TextNullableRowValue(rows.CorrelatedScalarNullableRowQuery(scalar))
	filter := correlatedNoteFilter{expr.NullableTextFilter(text), text}
	spec := reportSpec()
	spec.Columns[2] = DefineColumn[reportRecord](validation.DefineField("note", func(row ReportRow) value.Nullable[string] { return row.Note }), "reports.note",
		FilterBy(NullableWhere(foundryhttp.StringQuery[string](), filter).AllowLike()))
	manager, table := reportManager(t, fixture, spec, nil)
	actor := reportActor{Tenant: 7, Allowed: true}
	for _, tc := range []struct {
		op     Operator
		search string
		want   int64
	}{
		{InsensitiveContains, "ALP", 1}, {Like, "Al%", 1}, {Like, "al%", 0}, {InsensitiveContains, "%", 0},
	} {
		got, err := table.Query(t.Context(), manager, actor, Request{Filters: []Filter{{Column: "note", Op: tc.op, Values: []string{tc.search}}}})
		if err != nil || got.Total != tc.want || tc.want == 1 && (len(got.Items) != 1 || got.Items[0].ID != 6) {
			t.Fatalf("%s %q: %+v, %v", tc.op, tc.search, got, err)
		}
	}
	got, err := table.Query(t.Context(), manager, actor, Request{Filters: []Filter{{Column: "note", Op: IsNull}}})
	if err != nil || got.Total != 1 || got.Items[0].ID != 1 {
		t.Fatal("scalar NULL semantics", got, err)
	}
}
