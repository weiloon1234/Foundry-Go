package compilefail

import (
	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/model"
)

type admin struct{ ID int }
type authority struct{ Tenant int }

var otherTokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]]
var _ = datatable.RoutesConfig[admin, authority]{Tickets: datatable.TokenTickets(otherTokens)}
