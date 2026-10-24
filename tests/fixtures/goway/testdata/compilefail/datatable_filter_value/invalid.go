package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.Where[reporting.Member, string](foundryhttp.StringQuery[string](), expr.OrderRow(expr.Value(reporting.Members.Balance)))
