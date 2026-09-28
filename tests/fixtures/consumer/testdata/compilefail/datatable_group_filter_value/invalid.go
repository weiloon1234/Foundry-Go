package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.Having[reporting.Order, string](foundryhttp.StringQuery[string](), query.Count[reporting.Order]())
