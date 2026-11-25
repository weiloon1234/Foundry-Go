package invalid

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/datatable"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var config datatable.RoutesConfig[reporting.Operator, reporting.Authority]
var _, _ = datatable.Routes(http.Scope{}, reporting.NumericLevels, config)
