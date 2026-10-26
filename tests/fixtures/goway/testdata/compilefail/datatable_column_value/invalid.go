package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Member](validation.DefineField("name", func(r reporting.Member) string { return r.Name }), "reports.name").SortBy(expr.Row(expr.Value(reporting.Members.Balance)).Value())
