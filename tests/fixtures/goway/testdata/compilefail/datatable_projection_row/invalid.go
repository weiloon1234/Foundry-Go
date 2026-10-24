package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

var invalid = datatable.Spec[reporting.Member, reporting.Member, struct{}]{Row: contract.DefineJSON[reporting.OrderRow](contract.Schema{})}
