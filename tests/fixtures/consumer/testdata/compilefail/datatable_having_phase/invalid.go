package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var invalid = datatable.Where[reporting.Order, int64](foundryhttp.IntegerQuery[int64](), query.Count[reporting.Order]())
