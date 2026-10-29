package consumer

import b "github.com/weiloon1234/Foundry-Go/http/modelbinding"

var bound b.Endpoint[int, int, int, int, int]
var _ = bound.WithAuthorization(1)
var _ = bound.Validate()
var _ = bound.Description()
var boundActor b.AuthenticatedEndpoint[int, int, int, int, int, int]
var _ = boundActor.Validate()
var _ = boundActor.Description()
var _ = boundActor.WithAuthorization(1)
var resolver b.Resolver[int, int]
var _ = resolver.WithMissing(1)
var _ = resolver.Validate()
var _ = resolver.Resolve(1, 1)
var boundI b.IdempotentEndpoint[int, int, int, int, int]
var _ = boundI.Validate()
var _ = boundI.Description()
var _ = boundI.WithHeaders(1)
var boundActorI b.AuthenticatedIdempotentEndpoint[int, int, int, int, int, int]
var _ = boundActorI.Validate()
var _ = boundActorI.Description()
var _ = boundActorI.WithHeaders(1)
