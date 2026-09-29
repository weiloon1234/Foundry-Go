package http

import (
	"crypto/sha256"
	"encoding/hex"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

// WithActorMiddleware appends middleware that runs after authentication, access
// scopes and permissions, inside the authenticated request scope. Such
// middleware reads the concrete actor with guard.Require(r.Context()) (resolved
// once per scope, no second lookup), for example RateLimitByActor or tenant
// resolution. Middleware added with WithMiddleware runs before authentication.
func (e AuthenticatedEndpoint[P, Q, B, M, R]) WithActorMiddleware(middlewares ...Middleware) AuthenticatedEndpoint[P, Q, B, M, R] {
	e.authenticationBinding = e.authenticationBinding.withActorMiddleware(middlewares...)
	return e
}

// WithActorMiddleware appends middleware that runs after optional
// authentication; it reads the actor with guard.Optional(r.Context()).
func (e OptionalAuthenticationEndpoint[P, Q, B, M, R]) WithActorMiddleware(middlewares ...Middleware) OptionalAuthenticationEndpoint[P, Q, B, M, R] {
	e.required.authenticationBinding = e.required.authenticationBinding.withActorMiddleware(middlewares...)
	return e
}

// WithActorMiddleware appends middleware that runs after authentication and its
// requirements, before the native handler.
func (r AuthenticatedRoute[P, M]) WithActorMiddleware(middlewares ...Middleware) AuthenticatedRoute[P, M] {
	r.authenticationBinding = r.authenticationBinding.withActorMiddleware(middlewares...)
	return r
}

// WithActorMiddleware appends middleware that runs after optional authentication.
func (r OptionalAuthenticationRoute[P, M]) WithActorMiddleware(middlewares ...Middleware) OptionalAuthenticationRoute[P, M] {
	r.required.authenticationBinding = r.required.authenticationBinding.withActorMiddleware(middlewares...)
	return r
}

// ActorKey is a bounded quota key: a digest of the authenticated model identity,
// or the masked client network for anonymous requests. Declare actor limiters
// with ActorKeys().
type ActorKey string

// ActorKeys is the keyspace codec for ActorKey limiter declarations.
func ActorKeys() keyspace.Codec[ActorKey] { return keyspace.StringKeys[ActorKey]() }

// RateLimitByActor keys a quota by the authenticated model, so users behind one
// address do not share a budget and one user cannot escape it by changing
// address. Anonymous requests on optional routes fall back to the client IP
// (IPv4 per address, IPv6 per /64). Install it with WithActorMiddleware: before
// authentication there is no scope, and the request fails instead of silently
// falling back to IP limiting.
func RateLimitByActor[M model.Identifiable](limiter ratelimit.Limiter[ActorKey], guard auth.Guard[M]) Middleware {
	return RateLimitByActorWith(limiter, guard, DefaultIPRateLimitOptions())
}

// RateLimitByActorWith sets the anonymous fallback's IP grouping.
func RateLimitByActorWith[M model.Identifiable](limiter ratelimit.Limiter[ActorKey], guard auth.Guard[M], options IPRateLimitOptions) Middleware {
	compiled, err := options.compile()
	if err == nil {
		err = guard.Validate()
	}
	return rateLimitMiddleware(limiter, func(r *stdhttp.Request) (ActorKey, error) {
		subject, err := guard.Optional(r.Context())
		if err != nil {
			return "", err
		}
		if actor, present := subject.Get(); present {
			identity, err := actor.FoundryIdentity()
			if err != nil {
				return "", err
			}
			encoded, err := identity.KeyJSON()
			if err != nil {
				return "", err
			}
			digest := sha256.Sum256([]byte(identity.ModelName() + "\x00" + encoded))
			return ActorKey("actor:" + hex.EncodeToString(digest[:])), nil
		}
		ip := ClientIP(r.Context())
		if !ip.IsValid() {
			ip = PeerIP(r)
		}
		masked, err := compiled.key(ip)
		if err != nil {
			return "", err
		}
		return ActorKey("ip:" + masked.String()), nil
	}, err)
}
