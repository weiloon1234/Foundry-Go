package consumer

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Plain = h.Endpoint[int, int, int, int]

func effects(endpoint func() Plain, resolver, store, definition func() int, policy func() func() error, headers func() func() int) {
	operation := b.Idempotent(b.Bind(endpoint(), resolver()).WithAuthorization(policy()), store(), definition()).WithHeaders(headers())
	_ = operation.Handle(1, 2)
}

func shared(e Plain, resolver, store, definition int, policy func() error) {
	bound := b.Bind(e, resolver).WithAuthorization(policy)
	operation := b.Idempotent(bound, store, definition)
	_ = bound.Handle(1)
	_ = operation.Handle(1, 2)
}
