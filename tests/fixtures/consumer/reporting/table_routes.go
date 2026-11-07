package reporting

import (
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// NumericLevelRoutes uses framework-owned query, ticket and download contracts.
// The caller supplies current authority and token-owned tickets.
func NumericLevelRoutes(group http.Scope, config datatable.RoutesConfig[Operator, Authority], read, export []http.GuardOption[Operator]) ([]http.RouteRegistration, error) {
	// doc-example:begin datatable-7
	return datatable.Routes(group, NumericLevels, config,
		datatable.ReadAccess(read...),
		datatable.ExportAccess(export...),
	) // doc-example:end datatable-7
}

// NumericLevelTokenTickets retains the same operator type as NumericLevelRoutes.
func NumericLevelTokenTickets(tokens *token.Tokens[Operator, model.ID[Operator]]) datatable.Tickets[Operator] {
	// doc-example:begin tokens-example-1
	return datatable.TokenTickets(tokens)
	// doc-example:end tokens-example-1
}
