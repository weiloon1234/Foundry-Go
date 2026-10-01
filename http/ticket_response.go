package http

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/authtransport"
)

// TicketResponse delivers a single-use WebSocket handshake ticket from
// Tokens.IssueTicket through the same explicit credential boundary as
// TokenResponse: POST, TLS (or a configured TrustedProxy), no-store, and no
// delivery after the request ended. Mount it behind the issuing binding's guard.
// The body is {"ticket": ..., "expires_in": seconds}. The clock must agree with
// the token backend. Construction performs no I/O.
func TicketResponse[M, K any](status int, source clock.Clock) Response[token.Ticket[M, K]] {
	return credentialResponse(status, authtransport.TicketResponseJSON(), func() error { return credentialClockValid(source) },
		func(_ context.Context, ticket token.Ticket[M, K]) (authtransport.TicketResponse, error) {
			now, err := credentialTime(source)
			if err != nil {
				return authtransport.TicketResponse{}, err
			}
			if ticket.Family().IsZero() || !now.UTC().Before(ticket.ExpiresAt().UTC()) {
				return authtransport.TicketResponse{}, fault.New(fault.Invalid, "issued ticket is missing or expired")
			}
			if _, err := token.HashSecret(ticket.Secret()); err != nil {
				return authtransport.TicketResponse{}, err
			}
			return authtransport.TicketResponse{Ticket: ticket.Secret().Reveal(), ExpiresIn: int64(ticket.ExpiresAt().UTC().Sub(now.UTC()) / time.Second)}, nil
		})
}
