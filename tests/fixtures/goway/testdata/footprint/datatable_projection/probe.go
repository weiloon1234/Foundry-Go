package datatable_projection

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

var schema = contract.DefineJSON[Report](contract.Schema{Root: "foundry.test/goway/testdata/footprint/datatable_projection.Report", Types: []contract.Type{
	{ID: "foundry.test/goway/testdata/footprint/datatable_projection.Report", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "email", Type: "text", Required: true}, {Name: "age", Type: "integer", Required: true}}},
	{ID: "text", Kind: contract.StringKind}, {ID: "integer", Kind: contract.IntegerKind, Bits: 64, Signed: true},
}})
var table = datatable.Define(datatable.Spec[models.User, Report, struct{}]{
	ID: "reports.users", Row: schema, Exports: true,
	Columns: []datatable.ColumnRegistration[models.User, Report]{
		datatable.Text(validation.DefineBinding("email", func(r *Report) *string { return &r.Email }), "reports.email", models.Users.Email),
		datatable.Number(validation.DefineBinding("age", func(r *Report) *int { return &r.Age }), "reports.age", models.Users.Age, foundryhttp.IntegerQuery[int]()),
	},
	Filters:   []datatable.FilterRegistration[models.User]{datatable.DefineFilter("state", "reports.state", datatable.Where(foundryhttp.EnumQuery[models.Status, *models.Status](models.StatusActive.EnumDescriptor()), expr.CompareFilter(expr.CompareRow(expr.Value(models.Users.Status)))))},
	Stable:    []expr.ProjectionOrder[models.User]{expr.Asc(expr.Value(models.Users.ID))},
	Authorize: func(context.Context, struct{}, datatable.Action) error { return nil },
	Source: func(context.Context, struct{}) (rows.SelectionSource[models.User], error) {
		return rows.From(models.Users.Query()), nil
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
