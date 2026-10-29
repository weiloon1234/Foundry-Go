package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/value"
)

// PrepareAuthenticated configures the same typed preparation stage after credential
// admission and structural decoding. Authentication itself is never rerun here.
func PrepareAuthenticated[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R], prepare Preparation[P, Q, B]) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.endpoint = PrepareEndpoint(e.endpoint, prepare)
	return e
}
func PrepareOptional[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R], prepare Preparation[P, Q, B]) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	e.required.endpoint = PrepareEndpoint(e.required.endpoint, prepare)
	return e
}

// AuthorizeActor receives the selected guard's concrete actor and the
// prepared request before validation and domain/resource work, so a denied
// actor never reaches database-backed rules. Configure this before Signed or binding.
func AuthorizeActor[P, Q, B, M, R any](e AuthenticatedEndpoint[P, Q, B, M, R], authorize func(context.Context, M, Input[P, Q, B]) error) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.authorization = &authorize
	return e
}

// AuthorizeOptionalActor preserves the explicit absent-actor case. Invalid supplied
// credentials still fail admission before this optional-actor callback runs.
func AuthorizeOptionalActor[P, Q, B, M, R any](e OptionalAuthenticationEndpoint[P, Q, B, M, R], authorize func(context.Context, value.Optional[M], Input[P, Q, B]) error) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	e.authorization = &authorize
	return e
}
