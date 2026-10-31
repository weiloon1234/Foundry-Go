package migration_test

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// migration:column-options-v030
func usernameColumn() datatable.ColumnRegistration[Admin, AdminRow] {
	text := foundryhttp.StringQuery[string]()
	field := validation.DefineField("username", func(row AdminRow) string { return row.Username })
	username := expr.TextRow(orm.TextExpr(Admins.Username))
	return datatable.DefineColumn[Admin](field, "admins.fields.username",
		datatable.SortBy(expr.AsSelected(username)),
		datatable.FilterBy(datatable.Where(text, expr.TextFilter(username))),
		datatable.Searchable(username),
		datatable.ExportAs(expr.AsSelected(username), datatable.ScalarCell(text)),
	)
}

// migration:end
