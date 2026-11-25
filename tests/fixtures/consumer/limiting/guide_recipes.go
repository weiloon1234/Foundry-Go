package limiting

import (
	"context"
	"net/http"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
)

func takeMemberQuota(ctx context.Context, client ratelimit.Backend, namespace keyspace.Namespace, member Member, handleBusy func(time.Duration) error) error {
	// doc-example:begin take-member-quota
	store, err := ratelimit.NewStore(client, ratelimit.DefaultConfig(namespace))
	if err != nil {
		return err
	}
	limiter, err := MemberRequests.Bind(store)
	if err != nil {
		return err
	}
	decision, err := limiter.Take(ctx, member.ID, 5)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		// RetryAfter is relative to the authority's decision time.
		return handleBusy(decision.RetryAfter)
	}
	// doc-example:end take-member-quota
	return nil
}

func inspectMemberQuota(ctx context.Context, limiter ratelimit.Limiter[model.ID[Member]], member Member, sendVerification func(context.Context, Member) error) (ratelimit.Decision, uint32, time.Duration, bool, error) {
	// doc-example:begin inspect-member-quota
	decision, err := limiter.Peek(ctx, member.ID, 5)
	if err != nil {
		return decision, 0, 0, false, err
	}
	remaining, err := limiter.Remaining(ctx, member.ID)
	if err != nil {
		return decision, 0, 0, false, err
	}
	wait, err := limiter.AvailableIn(ctx, member.ID, 5)
	if err != nil {
		return decision, remaining, 0, false, err
	}
	cleared, err := limiter.Clear(ctx, member.ID)
	if err != nil {
		return decision, remaining, wait, false, err
	}
	decision, err = limiter.Attempt(ctx, member.ID, 1, func(ctx context.Context) error {
		return sendVerification(ctx, member)
	})
	// doc-example:end inspect-member-quota
	return decision, remaining, wait, cleared, err
}

func memberRateMiddleware(limiter ratelimit.Limiter[model.ID[Member]], currentMemberID func(context.Context) (model.ID[Member], error)) foundryhttp.Middleware {
	// doc-example:begin member-rate-middleware
	middleware := foundryhttp.RateLimit(limiter,
		func(r *http.Request) (model.ID[Member], error) {
			return currentMemberID(r.Context())
		},
	)
	// doc-example:end member-rate-middleware
	return middleware
}
