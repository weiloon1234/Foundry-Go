package invalid

import (
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func invalid(id string) { _ = datatable.Spec[reporting.Member, reporting.OrderRow, struct{}]{ID: id} }
