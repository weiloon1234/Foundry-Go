package datatable

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type NameTotalRow struct {
	Name    string `json:"name"`
	Members int64  `json:"members"`
}

func nameTotalJSON() contract.JSON[NameTotalRow] {
	const root contract.TypeID = "github.com/weiloon1234/Foundry-Go/datatable.NameTotalRow"
	return contract.DefineJSON[NameTotalRow](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "name", Type: "text", Required: true}, {Name: "members", Type: "integer", Required: true},
		}},
		{ID: "text", Kind: contract.StringKind},
		{ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
	}})
}

// nameTotalTable counts a tenant's report rows per name: its base source is
// tenant-scoped with WhereSource, grouped with GroupSource and limited by a fixed
// HavingSource.
func nameTotalTable() Table[reportRecord, NameTotalRow, reportActor] {
	_, name, _, _ := reportFields()
	members := expr.Count[reportRecord]()
	text, integer := foundryhttp.StringQuery[string](), foundryhttp.IntegerQuery[int64]()
	return Define(Spec[reportRecord, NameTotalRow, reportActor]{
		ID:  "reports.name_totals",
		Row: nameTotalJSON(),
		Columns: []ColumnRegistration[reportRecord, NameTotalRow]{
			Column(validation.DefineBinding("name", func(r *NameTotalRow) *string { return &r.Name }), "reports.name", expr.AsSelected(name), SortBy(expr.AsSelected(name)), FilterBy(Where(text, expr.TextFilter(name)))),
			Column(validation.DefineBinding("members", func(r *NameTotalRow) *int64 { return &r.Members }), "reports.members", expr.AsSelected(members), SortBy(expr.AsSelected(members)), FilterBy(Having(integer, expr.OrderValueFilter(members)))),
		},
		Stable:    []expr.ProjectionOrder[reportRecord]{expr.AscValue(expr.AsSelected(name))},
		Authorize: func(context.Context, reportActor, Action) error { return nil },
		Source: func(_ context.Context, actor reportActor) (rows.SelectionSource[reportRecord], error) {
			tenant := rows.WhereSource(rows.From(reportModels.Query()), expr.Eq(expr.Value(reportModels.TenantID), actor.Tenant))
			return rows.HavingSource(rows.GroupSource(tenant, expr.Group(name)), expr.GtValue(members, 1)), nil
		},
	})
}

// nameTotalRequest filters both phases: an OR group of aggregate filters must
// stay parenthesized beside the fixed HAVING, which it cannot bypass.
var nameTotalRequest = Request{
	Sort: []Sort{{Column: "members", Direction: Descending}},
	Filters: []Filter{
		{Op: Equal, Column: "name", Values: []string{"Ada"}},
		{Op: Any, Children: []Filter{{Op: Greater, Column: "members", Values: []string{"5"}}, {Op: Less, Column: "members", Values: []string{"3"}}}},
	},
}

// TestSourcePhaseOperationsReachTheTableQuery runs the grouped table's request
// path and pins the SQL each phase contributes.
func TestSourcePhaseOperationsReachTheTableQuery(t *testing.T) {
	table := nameTotalTable()
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	prepared, err := table.definition.prepare(nameTotalRequest, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := rows.CompileRead(rows.Read(source.rows))
	if err != nil {
		t.Fatal(err)
	}
	const want = `SELECT "report_rows"."name" AS "name", COUNT(*) AS "members" FROM "report_rows" WHERE ("report_rows"."deleted_at" IS NULL) AND ("report_rows"."tenant_id" = $1) AND ("report_rows"."name" = $2) GROUP BY "report_rows"."name" HAVING (COUNT(*) > $3) AND (((COUNT(*) > $4)) OR ((COUNT(*) < $5))) ORDER BY COUNT(*) DESC, "report_rows"."name" ASC`
	if statement.SQL() != want || !reflect.DeepEqual(statement.Arguments(), []any{int64(7), "Ada", int64(1), int64(5), int64(3)}) {
		t.Fatalf("source phases and request filters did not combine:\ngot  %s %#v\nwant %s", statement.SQL(), statement.Arguments(), want)
	}
	count, err := rows.CompileRead(rows.Read(source.count))
	if err != nil || !strings.Contains(count.SQL(), `GROUP BY "report_rows"."name" HAVING (COUNT(*) > $3) AND (((COUNT(*) > $4)) OR ((COUNT(*) < $5)))`) || !reflect.DeepEqual(count.Arguments(), statement.Arguments()) {
		t.Fatal("count lost a source phase", count.SQL(), err)
	}
}
