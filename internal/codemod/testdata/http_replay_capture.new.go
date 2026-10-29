package consumer

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Plain = h.Endpoint[int, int, int, int]

func effects(endpoint func() Plain, resolver, store, definition func() int, policy func() func() error, headers func() func() int) {
	replayEndpoint := endpoint()
	replayResolver := resolver()
	replayPolicy := policy()
	replayStore := store()
	replayDefinition := definition()
	operation := b.BindIdempotent(h.Idempotent(replayEndpoint, replayStore, replayDefinition), replayResolver)
	operation = b.AuthorizeIdempotentResource(operation, replayPolicy)
	operation = b.IdempotentHeaders(operation, headers())
	_ = operation.Handle(1, 2)
}

func shared(e Plain, resolver, store, definition int, policy func() error) {
	replayEndpoint0 := e
	replayResolver0 := resolver
	replayPolicy0 := policy
	bound := b.AuthorizeResource(b.Bind(h.ForBinding(replayEndpoint0), replayResolver0), replayPolicy0)
	replayStore0 := store
	replayDefinition0 := definition
	operation := b.BindIdempotent(h.Idempotent(replayEndpoint0, replayStore0, replayDefinition0), replayResolver0)
	operation = b.AuthorizeIdempotentResource(operation, replayPolicy0)
	_ = bound.Handle(1)
	_ = operation.Handle(1, 2)
}
