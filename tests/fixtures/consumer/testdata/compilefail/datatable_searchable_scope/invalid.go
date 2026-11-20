package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var name = expr.TextRow(reporting.MemberModels.Name)
var invalid = datatable.DefineColumn[reporting.Order, reporting.MemberRow, string](
	reporting.MemberRowValidationFields().Name,
	reporting.MemberNameLabel,
	datatable.Searchable(name),
)
