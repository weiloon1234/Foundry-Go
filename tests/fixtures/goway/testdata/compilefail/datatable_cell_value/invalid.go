package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var cell = datatable.ScalarCell(foundryhttp.IntegerQuery[int64]())
var invalid = datatable.Column(validation.DefineBinding("name", func(r *reporting.Member) *string { return &r.Name }), "reports.name", expr.AsSelected(expr.TextRow(reporting.Members.Name)), datatable.ExportAs[reporting.Member, string](expr.AsSelected(expr.TextRow(reporting.Members.Name)), cell))
