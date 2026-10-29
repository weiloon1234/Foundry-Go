package consumer

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	b "github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

type Plain = h.Endpoint[int, int, int, int]
type Signed = h.SignedEndpoint[int, int, int, int]
type Actor = h.AuthenticatedEndpoint[int, int, int, string, int]

func inline(e Plain, se Signed, a Actor, r, s, d int) {
	_ = b.Idempotent(b.Bind(e, r), s, d).Handle(1, 2)
	_ = b.Idempotent(b.Bind(se, r), s, d).Handle(1, 2)
	_ = b.IdempotentAuthenticated(b.BindAuthenticated(a, r), s, d).Handle(1, 2)
	_ = b.Idempotent[Plain](b.Bind[Plain](e, r), s, d).Handle(1, 2)
}

func ordinary(e Plain, r, s, d int, policy func() error, headers func() int) {
	bound := b.Bind(e, r).WithAuthorization(policy)
	operation := b.Idempotent(bound, s, d).WithHeaders(headers)
	_ = operation.Validate()
	_, _ = operation.Description()
	_ = operation.Handle(1, 2)
}

func signed(se Signed, r, s, d int, policy func() error) {
	bound := b.Bind(se, r).WithAuthorization(policy)
	operation := b.Idempotent(bound, s, d)
	_ = operation.Handle(1, 2)
}

func authenticated(a Actor, r, s, d int, policy func() error, headers func() int) {
	bound := b.BindAuthenticated(a, r).WithAuthorization(policy)
	operation := b.IdempotentAuthenticated(bound, s, d).WithHeaders(headers)
	_ = operation.Validate()
	_, _ = operation.Description()
	_ = operation.Handle(1, 2)
}

func policies(e Plain, r, s, d int, policy func() error, headers func() int) {
	operation := b.Idempotent(b.Bind(e, r).WithAuthorization(policy).WithAuthorization(nil), s, d).WithHeaders(headers).WithHeaders(nil)
	_ = operation.Handle(1, 2)
}
