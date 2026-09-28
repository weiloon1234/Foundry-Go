package invalid

import (
	"context"
	n "foundry.test/consumer/nestedbindings"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.Bind(n.Transport(foundryhttp.Public), n.Resolve(nil)).Handle(func(context.Context, modelbinding.Input[n.Path, foundryhttp.NoQuery, foundryhttp.NoBody, n.Project]) (n.Reply, error) {
	return n.Reply{}, nil
})
