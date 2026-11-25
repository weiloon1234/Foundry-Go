package reporting

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/datatable"
)

// doc-example:begin compile-table
func CompileMemberSearch(ctx context.Context, authority Authority) (datatable.Statements, error) {
	return datatable.Compile(ctx, Members, datatable.DefaultConfig(), authority, datatable.Request{Search: "Ada"})
}

// doc-example:end compile-table

// doc-example:begin datatable-2
func ExportTotals(ctx context.Context, manager *datatable.Manager, authority Authority) (*datatable.Artifact, error) {
	request := datatable.Request{Filters: []datatable.Filter{{Op: datatable.Greater, Column: "total", Values: []string{"10"}}}}
	return Totals.Export(ctx, manager, authority, request, datatable.ExportOptions{Format: datatable.CSV})
}

// doc-example:end datatable-2
