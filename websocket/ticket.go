package websocket

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TicketSubprotocolPrefix marks a single-use handshake ticket offered as one
// extra Sec-WebSocket-Protocol entry next to Subprotocol. The hub never selects
// or echoes it; only Subprotocol is negotiated.
const TicketSubprotocolPrefix = "foundry.ticket."

// MaxTicketRedeemers bounds the guards one hub accepts tickets for.
const MaxTicketRedeemers = auth.MaxCredentials

// TicketRedeemer exchanges a single-use handshake ticket for a bound credential
// of the guard whose credential source it names. token.Tokens implements it.
// An unknown, expired or already redeemed ticket is omitted, not an error.
type TicketRedeemer interface {
	TicketSource() auth.CredentialName
	RedeemTicket(context.Context, secret.String) (value.Optional[auth.BoundCredential], error)
}

// WithTickets lets browsers, which cannot send an Authorization header on a
// WebSocket, authenticate with a ticket from TicketSubprotocolPrefix. The hub
// redeems it after the origin check, trying redeemers in order; the first that
// knows it authenticates that guard's channels for the connection. Every later
// auth scope re-verifies the bound credential, so revocation still disconnects.
func WithTickets(redeemers ...TicketRedeemer) Option {
	return func(o *options) error {
		if len(redeemers) == 0 || len(redeemers)+len(o.tickets) > MaxTicketRedeemers {
			return fault.New(fault.Invalid, "WebSocket tickets require a bounded set of redeemers")
		}
		for _, redeemer := range redeemers {
			if redeemer == nil || !identifier.Semantic(string(redeemer.TicketSource())) {
				return fault.New(fault.Invalid, "WebSocket ticket redeemer requires a credential source")
			}
			for _, existing := range o.tickets {
				if existing.TicketSource() == redeemer.TicketSource() {
					return fault.New(fault.Duplicate, "WebSocket ticket credential source is repeated")
				}
			}
			o.tickets = append(o.tickets, redeemer)
		}
		return nil
	}
}

// offeredSubprotocols requires Subprotocol and returns at most one offered
// ticket. Offered entries are bounded before they are split.
func offeredSubprotocols(r *stdhttp.Request) (secret.String, bool) {
	bytes := 0
	for _, field := range r.Header.Values("Sec-WebSocket-Protocol") {
		bytes += len(field)
		if bytes > 1024 {
			return secret.String{}, false
		}
	}
	negotiated, tickets := false, 0
	var ticket secret.String
	for _, field := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, item := range strings.Split(field, ",") {
			item = strings.TrimSpace(item)
			if item == Subprotocol {
				negotiated = true
			}
			if raw, ok := strings.CutPrefix(item, TicketSubprotocolPrefix); ok {
				tickets++
				ticket = secret.New(raw)
			}
		}
	}
	if !negotiated || tickets > 1 || tickets == 1 && ticket.Reveal() == "" {
		return secret.String{}, false
	}
	return ticket, true
}

// redeemTicket binds an offered ticket into the handshake credentials. Every
// redeemer is tried until one knows the ticket, so one guard's failing store
// cannot block another guard's tickets. When none knows it the upgrade is
// rejected: 503 when a redeemer could not decide, otherwise 401. A ticket for a
// source already carrying a captured secret is ambiguous and rejected.
func (h *Hub) redeemTicket(r *stdhttp.Request, credentials auth.Credentials, ticket secret.String) (auth.Credentials, foundryhttp.ErrorCode) {
	if len(h.tickets) == 0 {
		return auth.Credentials{}, foundryhttp.Unauthenticated
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.config.OperationTimeout)
	defer cancel()
	failure := foundryhttp.Unauthenticated
	for _, redeemer := range h.tickets {
		var found value.Optional[auth.BoundCredential]
		var returned error
		if err := callback.Isolated("redeem WebSocket ticket", func() error {
			found, returned = redeemer.RedeemTicket(ctx, ticket)
			return nil
		}); err != nil {
			failure = foundryhttp.Unavailable
			continue
		}
		if returned != nil {
			if ticketFailure(returned) == foundryhttp.Unavailable {
				failure = foundryhttp.Unavailable
			}
			continue
		}
		bound, present := found.Get()
		if !present {
			continue
		}
		withTicket, err := credentials.WithBound(redeemer.TicketSource(), bound)
		if err != nil {
			return auth.Credentials{}, foundryhttp.Unauthenticated
		}
		return withTicket, ""
	}
	return auth.Credentials{}, failure
}

// ticketFailure classifies a redeemer error with bounded inspection. Only an
// authentication failure means the ticket is unusable; anything else, including
// an incomplete inspection, is a transient failure the client may retry.
func ticketFailure(err error) foundryhttp.ErrorCode {
	unauthenticated := false
	complete := errorgraph.Walk(err, func(current error) bool {
		unauthenticated = errorgraph.Matches(current, auth.Unauthenticated)
		return !unauthenticated
	})
	if complete && unauthenticated {
		return foundryhttp.Unauthenticated
	}
	return foundryhttp.Unavailable
}
