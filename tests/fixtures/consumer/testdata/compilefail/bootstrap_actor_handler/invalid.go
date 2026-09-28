package invalid

import (
	"context"
	"foundry.test/consumer/bootstrap"
	"github.com/weiloon1234/Foundry-Go/http"
)

func invalid(endpoint http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, bootstrap.Profile], group http.GuardBinding[bootstrap.Member]) {
	_ = http.Authenticated(endpoint, group).Handle(func(context.Context, bootstrap.Operator, bootstrap.MemberProfileInput) (bootstrap.Profile, error) {
		return bootstrap.Profile{}, nil
	})
}
