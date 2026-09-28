package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.Related[reporting.Member, reporting.Order, string](datatable.Where(foundryhttp.StringQuery[string](), reporting.OrderFields().Label), func(p query.Predicate[reporting.Member]) query.Predicate[reporting.Member] { return p })
