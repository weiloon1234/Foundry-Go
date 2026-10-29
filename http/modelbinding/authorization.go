package modelbinding

import (
	"context"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// AuthorizeResource configures a resource policy after the complete model
// bundle resolves. On framework endpoints it runs at the binding stage, before
// validation and domain work. Request-only authorization belongs on the
// original HTTP endpoint and runs before any model lookups.
func AuthorizeResource[P, Q, B, M, R any](e Endpoint[P, Q, B, M, R], authorize func(context.Context, Input[P, Q, B, M]) error) Endpoint[P, Q, B, M, R] {
	e.authorization = &authorize
	return e
}

// AuthorizeAuthenticatedResource preserves both the concrete guard actor (including optional
// actors) and the complete bound resource. Relationship matching is not a grant.
func AuthorizeAuthenticatedResource[P, Q, B, S, M, R any](e AuthenticatedEndpoint[P, Q, B, S, M, R], authorize func(context.Context, S, Input[P, Q, B, M]) error) AuthenticatedEndpoint[P, Q, B, S, M, R] {
	e.authorization = &authorize
	return e
}

func authorizeResource(ctx context.Context, authorize func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var returned error
	failure := callback.Isolated("HTTP bound resource authorization", func() error { returned = authorize(); return nil })
	if failure != nil {
		return foundryhttp.InternalError.WithCause(failure)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return returned
}
