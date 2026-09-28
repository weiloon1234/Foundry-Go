package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/model"
)

const RequestIDHeader = "X-Request-ID"

// RequestID returns the request's typed correlation ID from shared attribution.
// Outside a request/attributed operation it returns the empty ID. HTTP generates
// its own ID rather than trusting an incoming correlation header.
func RequestID(ctx context.Context) attribution.RequestID {
	return attribution.FromContext(ctx).Request().ID
}

type requestIdentity struct{}

// prepareRequest establishes fresh anonymous request attribution. It never
// inherits an application-level subject or trusts forwarding headers. Guards
// may later enrich this same immutable origin with a concrete authenticated model.
func prepareRequest(r *stdhttp.Request) (*stdhttp.Request, error) {
	id, err := model.NewID[requestIdentity]()
	if err != nil {
		return r, InternalError.WithCause(err)
	}
	metadata := attribution.Request{ID: attribution.RequestID(id.String())}
	origin, err := (attribution.Origin{}).WithRequest(metadata)
	if err != nil {
		return r, InternalError.WithCause(err)
	}
	ctx, err := attribution.WithContext(r.Context(), origin)
	if err != nil {
		return r, InternalError.WithCause(err)
	}
	r = r.WithContext(ctx)
	// Keep the generated ID even when the remaining request metadata is invalid.
	metadata.UserAgent = r.UserAgent()
	metadata.IP = PeerIP(r)
	origin, err = origin.WithRequest(metadata)
	if err != nil {
		return r, BadRequest.WithCause(err)
	}
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		return r, InternalError.WithCause(err)
	}
	return r.WithContext(ctx), nil
}
