package auth

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TicketRedeemer exchanges a single-use handshake ticket for a bound credential
// of the guard whose credential source it names. token.Tokens implements it.
// An unknown, expired or already redeemed ticket is omitted, not an error.
type TicketRedeemer interface {
	// TicketSource returns the credential source whose tickets this redeemer accepts.
	TicketSource() CredentialName
	// RedeemTicket consumes a single-use handshake ticket, returning omission for unknown,
	// expired or already redeemed tickets.
	RedeemTicket(context.Context, secret.String) (value.Optional[BoundCredential], error)
}
