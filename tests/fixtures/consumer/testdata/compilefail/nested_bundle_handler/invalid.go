package invalid

import (
	"context"

	n "foundry.test/consumer/nestedbindings"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var endpoint = n.Transport(foundryhttp.Public)
var binding = foundryhttp.ForBinding(endpoint)
var resolver = n.Resolve(nil)

var _ = modelbinding.Bind(binding, resolver).Handle(func(context.Context, modelbinding.Input[n.Path, foundryhttp.NoQuery, foundryhttp.NoBody, n.Project]) (n.Reply, error) {
	return n.Reply{}, nil
})
