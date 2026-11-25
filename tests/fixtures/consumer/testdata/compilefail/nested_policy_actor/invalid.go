package invalid

import (
	"context"

	n "foundry.test/consumer/nestedbindings"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var endpoint = n.Transport(foundryhttp.Guarded)
var teams foundryhttp.GuardBinding[n.Team]
var secured = foundryhttp.Authenticated(endpoint, teams, teams.AnyActor())
var binding = foundryhttp.ForAuthenticatedBinding(secured)
var resolver = n.Resolve(nil)
var bound = modelbinding.BindAuthenticated(binding, resolver)

var _ = modelbinding.AuthorizeAuthenticatedResource[n.Path, foundryhttp.NoQuery, foundryhttp.NoBody, n.Team, n.Resources, n.Reply](bound, func(context.Context, string, n.Request) error { return nil })
