package datatable_projection

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type Report struct {
	Email string `json:"email"`
	Age   int    `json:"age"`
}
type fields struct {
	rows.Row[Report]
	Email rows.Field[Report, string]
	Age   rows.Field[Report, int]
}

var output = rows.Define(func(b rows.Builder[Report], r *Report) fields {
	return fields{b.Row(), rows.TextOf(b, &r.Email), rows.NumberOf(b, &r.Age)}
})
var schema = contract.DefineJSON[Report](contract.Schema{Root: "foundry.test/goway/testdata/footprint/datatable_projection.Report", Types: []contract.Type{
	{ID: "foundry.test/goway/testdata/footprint/datatable_projection.Report", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "email", Type: "text", Required: true}, {Name: "age", Type: "integer", Required: true}}},
	{ID: "text", Kind: contract.StringKind}, {ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
}})
var table = datatable.Define(datatable.Spec[models.User, Report, struct{}]{
	ID: "reports.users", Row: schema, Exports: true,
	Columns: []datatable.ColumnRegistration[models.User, Report]{
		datatable.DefineColumn[models.User](validation.DefineField("email", func(r Report) string { return r.Email }), "reports.email",
			datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(models.Users.Email))))),
			datatable.FilterBy(datatable.Where(foundryhttp.StringQuery[string](), expr.TextFilter(expr.TextRow(orm.TextExpr(models.Users.Email))))),
			datatable.Searchable(expr.TextRow(orm.TextExpr(models.Users.Email))),
			datatable.ExportAs(expr.AsSelected(expr.TextRow(orm.TextExpr(models.Users.Email))), datatable.ScalarCell(foundryhttp.StringQuery[string]()))),
		datatable.DefineColumn[models.User](validation.DefineField("age", func(r Report) int { return r.Age }), "reports.age",
			datatable.SortBy(expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(models.Users.Age))))),
			datatable.FilterBy(datatable.Where(foundryhttp.IntegerQuery[int](), expr.OrderFilter(expr.OrderRow(expr.Value(orm.NumberExpr(models.Users.Age)))))),
			datatable.ExportAs(expr.AsSelected(expr.OrderRow(expr.Value(orm.NumberExpr(models.Users.Age)))), datatable.ScalarCell(foundryhttp.IntegerQuery[int]()))),
	},
	Filters:   []datatable.FilterRegistration[models.User]{datatable.DefineFilter("state", "reports.state", datatable.Where(foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()), expr.CompareFilter(expr.CompareRow(expr.Value(orm.EnumExpr(models.Users.Status))))))},
	Stable:    []expr.ProjectionOrder[models.User]{expr.Asc(expr.Value(orm.KeyExpr(models.Users.ID)))},
	Authorize: func(context.Context, struct{}, datatable.Action) error { return nil },
	Source: func(context.Context, struct{}) (rows.Query[models.User, Report], error) {
		return rows.Select(rows.From(models.Users.Query()), output, rows.Map(output.Email, expr.Value(orm.TextExpr(models.Users.Email))), rows.Map(output.Age, expr.Value(orm.NumberExpr(models.Users.Age)))), nil
	},
})

func Probe(ctx context.Context, manager *datatable.Manager) {
	_, _ = table.Query(ctx, manager, struct{}{}, datatable.Request{})
	_, _ = table.SimpleQuery(ctx, manager, struct{}{}, datatable.Request{})
	_, _ = table.Count(ctx, manager, struct{}{}, datatable.Request{})
	artifact, err := table.Export(ctx, manager, struct{}{}, datatable.Request{}, datatable.ExportOptions{Format: datatable.CSV})
	if err == nil {
		_ = artifact.Close()
	}
}
