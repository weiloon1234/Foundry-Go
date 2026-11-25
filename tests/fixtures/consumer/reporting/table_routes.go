package reporting

import (
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin datatable-7
// NumericLevelRoutes uses framework-owned query, ticket and download contracts.
// The caller supplies current authority and token-owned tickets.
func NumericLevelRoutes(group foundryhttp.Scope, config datatable.RoutesConfig[Operator, Authority], read, export []foundryhttp.GuardOption[Operator]) ([]foundryhttp.RouteRegistration, error) {
	return datatable.Routes(group, NumericLevels, config,
		datatable.ReadAccess(read...),
		datatable.ExportAccess(export...),
	)
}

// doc-example:end datatable-7

// OperatorTokenTickets retains the same operator type as NumericLevelRoutes.
// doc-example:begin tokens-example-1
func OperatorTokenTickets(tokens *token.Tokens[Operator, model.ID[Operator]]) datatable.Tickets[Operator] {
	return datatable.TokenTickets(tokens)
}

// doc-example:end tokens-example-1
