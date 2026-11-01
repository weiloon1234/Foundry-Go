package websocket

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/authticket"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// TicketSubprotocolPrefix marks a single-use handshake ticket offered as one
// extra Sec-WebSocket-Protocol entry next to Subprotocol. The hub never selects
// or echoes it; only Subprotocol is negotiated.
const TicketSubprotocolPrefix = "foundry.ticket."

// MaxTicketRedeemers bounds the guards one hub accepts tickets for.
const MaxTicketRedeemers = auth.MaxCredentials

// WithTickets lets browsers, which cannot send an Authorization header on a
// WebSocket, authenticate with a ticket from TicketSubprotocolPrefix. The hub
// redeems it after the origin check, trying redeemers in order; the first that
// knows it authenticates that guard's channels for the connection. Every later
// auth scope re-verifies the bound credential, so revocation still disconnects.
func WithTickets(redeemers ...auth.TicketRedeemer) Option {
	return func(o *options) error {
		if len(redeemers) == 0 || len(redeemers)+len(o.tickets) > MaxTicketRedeemers {
			return safeerror.NewFault(fault.Invalid, "WebSocket tickets require a bounded set of redeemers")
		}
		for _, redeemer := range redeemers {
			if redeemer == nil || !identifier.Semantic(string(redeemer.TicketSource())) {
				return safeerror.NewFault(fault.Invalid, "WebSocket ticket redeemer requires a credential source")
			}
			for _, existing := range o.tickets {
				if existing.TicketSource() == redeemer.TicketSource() {
					return safeerror.NewFault(fault.Duplicate, "WebSocket ticket credential source is repeated")
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
	credentials, outcome := authticket.Redeem(ctx, credentials, ticket, h.tickets...)
	switch outcome {
	case authticket.Success:
		return credentials, ""
	case authticket.Unavailable:
		return auth.Credentials{}, foundryhttp.Unavailable
	default:
		return auth.Credentials{}, foundryhttp.Unauthenticated
	}
}
