package invalid

import (
	"context"
	"foundry.test/goway/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

type authority struct{ Tenant int64 }

var table datatable.Table[reporting.Member, reporting.OrderRow, authority]

func invalid(manager *datatable.Manager, actor reporting.Member) {
	_, _ = table.Query(context.Background(), manager, actor, datatable.Request{})
}
