// Package limiting proves typed domain and HTTP rate-limit usage from a consumer.
package limiting

import (
	"context"
	"foundry.test/consumer/mutatorqueries"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	stdhttp "net/http"
	"net/netip"
)

// MemberRequests is shared across explicitly selected member routes and kernels.
var MemberRequests = ratelimit.Define("member-requests", keyspace.TextKeys[model.ID[mutatorqueries.Member]](), ratelimit.PerMinute(60))
var LoginIPs = ratelimit.Define("login-ip", keyspace.TextKeys[netip.Addr](), ratelimit.PerMinute(10))

type MemberLimiter = ratelimit.Limiter[model.ID[mutatorqueries.Member]]

func Bind(store *ratelimit.Store) (MemberLimiter, error) { return MemberRequests.Bind(store) }

// AdmitExport consumes an explicit cost using the same model-owned quota.
func AdmitExport(ctx context.Context, limiter MemberLimiter, id model.ID[mutatorqueries.Member], cost uint32) (ratelimit.Decision, error) {
	return limiter.Take(ctx, id, cost)
}

// MemberMiddleware accepts the application's concrete model-key resolver. Future
// auth integration can resolve it from the authenticated model without an Actor.
func MemberMiddleware(limiter MemberLimiter, resolve func(*stdhttp.Request) (model.ID[mutatorqueries.Member], error)) foundryhttp.Middleware {
	return foundryhttp.RateLimit(limiter, resolve)
}
func LoginMiddleware(store *ratelimit.Store) (foundryhttp.Middleware, error) {
	limiter, err := LoginIPs.Bind(store)
	if err != nil {
		return foundryhttp.Middleware{}, err
	}
	return foundryhttp.RateLimitByIP(limiter), nil
}
