package limiting

import (
	"context"
	stdhttp "net/http"
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

func inspectMemberQuota(ctx context.Context, limiter ratelimit.Limiter[model.ID[Member]], member Member, sendVerification func(context.Context, Member) error) error {
	// doc-example:begin inspect-member-quota
	decision, err := limiter.Peek(ctx, member.ID, 5) // non-consuming Decision
	if err != nil {
		return err
	}
	remaining, err := limiter.Remaining(ctx, member.ID) // uint32
	if err != nil {
		return err
	}
	wait, err := limiter.AvailableIn(ctx, member.ID, 5) // time.Duration, 0 if it fits now
	if err != nil {
		return err
	}
	cleared, err := limiter.Clear(ctx, member.ID) // bool
	if err != nil {
		return err
	}
	decision, err = limiter.Attempt(ctx, member.ID, 1, func(ctx context.Context) error {
		return sendVerification(ctx, member)
	})
	if err != nil {
		return err
	}
	// doc-example:end inspect-member-quota
	_, _, _, _ = decision, remaining, wait, cleared
	return nil
}

func memberRateMiddleware(limiter ratelimit.Limiter[model.ID[Member]], currentMemberID func(context.Context) (model.ID[Member], error)) foundryhttp.Middleware {
	// doc-example:begin member-rate-middleware
	middleware := foundryhttp.RateLimit(limiter,
		func(r *stdhttp.Request) (model.ID[Member], error) {
			return currentMemberID(r.Context())
		},
	)
	// doc-example:end member-rate-middleware
	return middleware
}
