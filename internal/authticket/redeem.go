// Package authticket owns single-use credential redemption for HTTP and WebSocket.
package authticket

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Outcome classifies redemption without depending on a transport.
type Outcome uint8

const (
	// Success means a ticket produced a bound credential.
	Success Outcome = iota
	// Unauthenticated means no redeemer recognized a usable ticket.
	Unauthenticated
	// Unavailable means a redeemer could not decide whether the ticket was valid.
	Unavailable
)

// Redeem tries each source until one recognizes the ticket. An undecided source
// makes total failure unavailable, while a later recognized ticket still wins.
func Redeem(ctx context.Context, credentials auth.Credentials, ticket secret.String, redeemers ...auth.TicketRedeemer) (auth.Credentials, Outcome) {
	failure := Unauthenticated
	for _, redeemer := range redeemers {
		var found value.Optional[auth.BoundCredential]
		var returned error
		var source auth.CredentialName
		if err := callback.Isolated("redeem authentication ticket", func() error {
			found, returned = redeemer.RedeemTicket(ctx, ticket)
			source = redeemer.TicketSource()
			return nil
		}); err != nil {
			failure = Unavailable
			continue
		}
		if returned != nil {
			if failureCode(returned) == Unavailable {
				failure = Unavailable
			}
			continue
		}
		bound, present := found.Get()
		if !present {
			continue
		}
		withTicket, err := credentials.WithBound(source, bound)
		if err != nil {
			return auth.Credentials{}, Unauthenticated
		}
		return withTicket, Success
	}
	return auth.Credentials{}, failure
}

// failureCode classifies a redeemer error with bounded inspection. Only an
// authentication failure means the ticket is unusable; anything else, including
// an incomplete inspection, is a transient failure the client may retry.
func failureCode(err error) Outcome {
	unauthenticated := false
	complete := callback.ErrorWalk(err, func(current error) bool {
		unauthenticated = errorgraph.Matches(current, auth.Unauthenticated)
		return !unauthenticated
	})
	if complete && unauthenticated {
		return Unauthenticated
	}
	return Unavailable
}
