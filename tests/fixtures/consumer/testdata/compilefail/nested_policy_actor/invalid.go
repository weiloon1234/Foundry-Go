package invalid

import (
	"context"
	n "foundry.test/consumer/nestedbindings"
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.BindAuthenticated(foundryhttp.RequireAuthentication(n.Transport(foundryhttp.Guarded), nil, auth.Guard[n.Team]{}), n.Resolve(nil)).WithAuthorization(func(context.Context, string, n.Request) error { return nil })
