package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var invalid = datatable.DefineColumn[reporting.Member, reporting.Member, string](validation.DefineField("name", func(r reporting.Member) string { return r.Name }), "reports.name", datatable.ExportAs[reporting.Member, string](expr.AsSelected(expr.TextRow(orm.TextExpr(reporting.Members.Name))), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())))
