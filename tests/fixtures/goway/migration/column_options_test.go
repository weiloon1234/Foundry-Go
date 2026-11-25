package migration_test

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin column-options-v030
func usernameColumn() datatable.ColumnRegistration[Admin, AdminRow] {
	text := foundryhttp.StringQuery[string]()
	field := validation.DefineBinding("username", func(row *AdminRow) *string { return &row.Username })
	username := expr.TextRow(Admins.Username)
	return datatable.Column(field, "admins.fields.username", expr.AsSelected(username),
		datatable.SortBy(expr.AsSelected(username)),
		datatable.FilterBy(datatable.Where(text, expr.TextFilter(username))),
		datatable.Searchable(username),
		datatable.ExportAs(expr.AsSelected(username), datatable.ScalarCell(text)),
	)
}

// doc-example:end column-options-v030
