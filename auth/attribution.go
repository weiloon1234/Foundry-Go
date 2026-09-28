package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attribution"
)

// Origin captures provenance from this guard's verified identity and the current
// request metadata. It reuses the scope's existing resolution, without another
// provider lookup or model getter. Pending/absent/invalid credentials fail; an
// inherited model/system origin never substitutes for this guard's verification.
// The result is serializable metadata, not authority for a later job or message.
func (g Guard[M]) Origin(ctx context.Context) (attribution.Origin, error) {
	result, err := g.resolve(ctx)
	if err != nil {
		return attribution.Origin{}, err
	}
	if !result.subject.IsSet() {
		return attribution.Origin{}, Unauthenticated
	}
	origin, err := (attribution.Origin{}).WithRequest(attribution.FromContext(ctx).Request())
	if err != nil {
		return attribution.Origin{}, err
	}
	origin, err = origin.WithIdentity(result.identity)
	if err != nil {
		return attribution.Origin{}, err
	}
	return origin.WithGuard(attribution.GuardName(g.Name()))
}

// WithAttribution derives a context for audit/event work inside the current
// authorization scope. Cancellation and existing context values are preserved.
// HTTP typed authentication adapters do this automatically before input decoding.
// For deferred work capture Origin only, then create a fresh execution context and
// independently select/verify its authority. Never retain the authentication scope.
func (g Guard[M]) WithAttribution(ctx context.Context) (context.Context, error) {
	origin, err := g.Origin(ctx)
	if err != nil {
		return nil, err
	}
	return attribution.WithContext(ctx, origin)
}
