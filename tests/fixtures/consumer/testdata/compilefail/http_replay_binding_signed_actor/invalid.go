package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Actor struct{ ID int }
type Resource struct{ ID int }

var endpoint h.SignedAuthenticatedEndpoint[h.NoPath, h.NoQuery, h.NoBody, Actor, h.NoContent]
var _ = modelbinding.BindAuthenticatedIdempotent[h.NoPath, h.NoQuery, h.NoBody, Actor, Resource, h.NoContent](endpoint, modelbinding.Resolver[h.NoPath, Resource]{})
