package http

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/value"
)

// WithPreparation configures the same typed preparation stage after credential
// admission and structural decoding. Authentication itself is never rerun here.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) WithPreparation(prepare Preparation[P, Q, B]) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.endpoint = e.endpoint.WithPreparation(prepare)
	return e
}
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) WithPreparation(prepare Preparation[P, Q, B]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	e.required.endpoint = e.required.endpoint.WithPreparation(prepare)
	return e
}

// WithAuthorization receives the selected guard's concrete actor and validated
// request before domain/resource work. Configure this before Signed or binding.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) WithAuthorization(authorize func(context.Context, M, Input[P, Q, B]) error) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.authorization = &authorize
	return e
}

// WithAuthorization preserves the explicit absent-actor case. Invalid supplied
// credentials still fail admission before this optional-actor callback runs.
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) WithAuthorization(authorize func(context.Context, value.Optional[M], Input[P, Q, B]) error) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	e.authorization = &authorize
	return e
}
