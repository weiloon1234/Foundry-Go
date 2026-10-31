package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.DefineColumn[reporting.Member, reporting.MemberRow, string](reporting.MemberRowValidationFields().Name, reporting.MemberNameLabel, datatable.ExportAs[reporting.Member, string](expr.AsSelected(expr.TextRow(orm.TextExpr(reporting.MemberModels.Name))), datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())))
