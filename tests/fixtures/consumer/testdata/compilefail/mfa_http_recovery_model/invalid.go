package invalid

import (
	"context"
	"foundry.test/consumer/multifactor"
	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	h "github.com/weiloon1234/Foundry-Go/http"
)

func wrong() {
	r := h.DefineRoute(h.RouteSpec{ID: "mfa", Method: h.POST, Access: h.Public}, h.StaticPath("/mfa"))
	e := h.DefineEndpoint(r, h.EmptyQuery(), h.EmptyBody(), h.MFARecoveryResponse[multifactor.Account](200))
	e.Handle(func(context.Context, h.Input[h.NoPath, h.NoQuery, h.NoBody]) (mfa.RecoveryCodes[recovering.Member], error) {
		return mfa.RecoveryCodes[recovering.Member]{}, nil
	})
}
