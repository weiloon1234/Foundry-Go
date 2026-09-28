package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/clock"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func wrong() {
	r := h.DefineRoute(h.RouteSpec{ID: "mfa", Method: h.POST, Access: h.Public}, h.StaticPath("/mfa"))
	e := h.DefineEndpoint(r, h.EmptyQuery(), h.EmptyBody(), h.MFAEnrollmentResponse[multifactor.Account](200, clock.System{}))
	e.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (mfa.Enrollment[recovering.Member], error) {
		return mfa.Enrollment[recovering.Member]{}, nil
	})
}
