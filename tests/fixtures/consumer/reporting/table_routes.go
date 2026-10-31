package reporting

import (
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http"
)

// NumericLevelRoutes uses framework-owned query, ticket and download contracts.
// The caller supplies current authority and application-owned ticket callbacks.
func NumericLevelRoutes(group http.Scope, config datatable.RoutesConfig[Operator, Authority], read, export []http.GuardOption[Operator]) ([]http.RouteRegistration, error) {
	return datatable.Routes(group, NumericLevels, config,
		datatable.ReadAccess(read...),
		datatable.ExportAccess(export...),
	)
}
