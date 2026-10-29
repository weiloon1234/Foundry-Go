package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Actor struct{ ID int }
type Resource struct{ ID int }

// A staged binding capability is not a transaction/replay capability.
var endpoint modelbinding.Transport[h.NoPath, h.NoQuery, h.NoBody, h.NoContent]
var _ = modelbinding.BindIdempotent[h.NoPath, h.NoQuery, h.NoBody, Resource, h.NoContent](endpoint, modelbinding.Resolver[h.NoPath, Resource]{})
