package compilefail

import (
	"foundry.test/goway/models/identity"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/model"
)

type admin struct{ ID int }
type authority struct{ Tenant int }

var otherTokens *token.Tokens[identity.Member, model.ID[identity.Member]]
var _ = datatable.RoutesConfig[admin, authority]{Tickets: datatable.TokenTickets(otherTokens)}
