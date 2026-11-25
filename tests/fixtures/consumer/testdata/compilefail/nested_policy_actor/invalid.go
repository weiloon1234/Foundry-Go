package invalid

import (
	"context"

	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var endpoint = n.Transport(foundryhttp.Guarded)
var guard auth.Guard[n.Team]
var secured = foundryhttp.RequireAuthentication(endpoint, nil, guard, foundryhttp.AnyActorOf(guard))
var binding = foundryhttp.ForAuthenticatedBinding(secured)
var resolver = n.Resolve(nil)
var bound = modelbinding.BindAuthenticated(binding, resolver)

var _ = modelbinding.AuthorizeAuthenticatedResource[n.Path, foundryhttp.NoQuery, foundryhttp.NoBody, n.Team, n.Resources, n.Reply](bound, func(context.Context, string, n.Request) error { return nil })
