package consumer

import b "github.com/weiloon1234/Foundry-Go/http/modelbinding"

var bound b.Endpoint[int, int, int, int, int]
var _ = b.AuthorizeResource(bound, 1)
var _ = b.ValidateEndpoint(bound)
var _ = b.DescribeEndpoint(bound)
var boundActor b.AuthenticatedEndpoint[int, int, int, int, int, int]
var _ = b.ValidateAuthenticatedEndpoint(boundActor)
var _ = b.DescribeAuthenticatedEndpoint(boundActor)
var _ = b.AuthorizeAuthenticatedResource(boundActor, 1)
var resolver b.Resolver[int, int]
var _ = b.WithMissing(resolver, 1)
var _ = b.ValidateResolver(resolver)
var _ = b.Resolve(resolver, 1, 1)
var boundI b.IdempotentEndpoint[int, int, int, int, int]
var _ = b.ValidateIdempotentEndpoint(boundI)
var _ = b.DescribeIdempotentEndpoint(boundI)
var _ = b.IdempotentHeaders(boundI, 1)
var boundActorI b.AuthenticatedIdempotentEndpoint[int, int, int, int, int, int]
var _ = b.ValidateAuthenticatedIdempotentEndpoint(boundActorI)
var _ = b.DescribeAuthenticatedIdempotentEndpoint(boundActorI)
var _ = b.AuthenticatedIdempotentHeaders(boundActorI, 1)
