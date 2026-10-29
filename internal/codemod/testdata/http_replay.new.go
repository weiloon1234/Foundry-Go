package consumer

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Plain = h.Endpoint[int, int, int, int]
type Signed = h.SignedEndpoint[int, int, int, int]
type Actor = h.AuthenticatedEndpoint[int, int, int, string, int]

func inline(e Plain, se Signed, a Actor, r, s, d int) {
	_ = b.BindIdempotent(h.Idempotent(e, s, d), r).Handle(1, 2)
	_ = b.BindIdempotent(h.IdempotentSigned(se, s, d), r).Handle(1, 2)
	_ = b.BindAuthenticatedIdempotent(h.IdempotentAuthenticated(a, s, d), r).Handle(1, 2)
	_ = b.BindIdempotent(h.Idempotent(e, s, d), r).Handle(1, 2)
}

func ordinary(e Plain, r, s, d int, policy func() error, headers func() int) {
	operation := b.BindIdempotent(h.Idempotent(e, s, d), r)
	operation = b.AuthorizeIdempotentResource(operation, policy)
	operation = b.IdempotentHeaders(operation, headers)
	_ = b.ValidateIdempotentEndpoint(operation)
	_, _ = b.DescribeIdempotentEndpoint(operation)
	_ = operation.Handle(1, 2)
}

func signed(se Signed, r, s, d int, policy func() error) {
	operation := b.BindIdempotent(h.IdempotentSigned(se, s, d), r)
	operation = b.AuthorizeIdempotentResource(operation, policy)
	_ = operation.Handle(1, 2)
}

func authenticated(a Actor, r, s, d int, policy func() error, headers func() int) {
	operation := b.BindAuthenticatedIdempotent(h.IdempotentAuthenticated(a, s, d), r)
	operation = b.AuthorizeAuthenticatedIdempotentResource(operation, policy)
	operation = b.AuthenticatedIdempotentHeaders(operation, headers)
	_ = b.ValidateAuthenticatedIdempotentEndpoint(operation)
	_, _ = b.DescribeAuthenticatedIdempotentEndpoint(operation)
	_ = operation.Handle(1, 2)
}

func policies(e Plain, r, s, d int, policy func() error, headers func() int) {
	operation := b.BindIdempotent(h.Idempotent(e, s, d), r)
	operation = b.AuthorizeIdempotentResource(operation, policy)
	operation = b.AuthorizeIdempotentResource(operation, nil)
	operation = b.IdempotentHeaders(operation, headers)
	operation = b.IdempotentHeaders(operation, nil)
	_ = operation.Handle(1, 2)
}
