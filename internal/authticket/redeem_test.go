package authticket

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

type failedRedeemer struct{ err error }

func (failedRedeemer) TicketSource() auth.CredentialName { return "test" }
func (r failedRedeemer) RedeemTicket(context.Context, secret.String) (value.Optional[auth.BoundCredential], error) {
	return value.Optional[auth.BoundCredential]{}, r.err
}

func TestRedemptionContainsErrorMethods(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		_, outcome := Redeem(t.Context(), auth.Credentials{}, secret.New("private"), failedRedeemer{cause})
		if outcome != Unavailable || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("unsafe error inspection")
		}
	}
	_, outcome := Redeem(t.Context(), auth.Credentials{}, secret.New("private"), failedRedeemer{auth.Unauthenticated})
	if outcome != Unauthenticated {
		t.Fatal("authentication failure classification")
	}
}
