package invalid

import (
	"context"
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
)

func invalid(manager *datatable.Manager, actor reporting.Operator) {
	_, _ = reporting.Members.Query(context.Background(), manager, actor, datatable.Request{})
}
