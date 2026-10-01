package token

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Ticket is a single-use handshake credential for the token family that
// authenticated its issuing request, for transports that cannot send an
// Authorization header, such as a browser WebSocket. Secret() is an explicit
// transport boundary; routine formatting and JSON never disclose it.
type Ticket[M, K any] struct {
	secret  secret.String
	expires temporal.DateTime
	family  ID[M]
}

func (t Ticket[M, K]) Secret() secret.String        { return t.secret }
func (t Ticket[M, K]) ExpiresAt() temporal.DateTime { return t.expires }
func (t Ticket[M, K]) Family() ID[M]                { return t.family }
func (Ticket[M, K]) Format(s fmt.State, _ rune)     { _, _ = s.Write([]byte("token ticket")) }

// familyBinding is the server-side stand-in for a redeemed ticket. It names a
// family and subject, so it is only ever created by this package's binder.
type familyBinding struct {
	family  model.ID[Record]
	subject model.Identity
}

func (t *Tokens[M, K]) ticketBackend() (TicketBackend, error) {
	if t.store.config.TicketLifetime == 0 {
		return nil, fault.New(fault.Invalid, "token tickets are disabled")
	}
	backend, ok := t.store.backend.(TicketBackend)
	if !ok {
		return nil, fault.New(fault.Invalid, "token backend does not support tickets")
	}
	return backend, nil
}

// IssueTicket mints a single-use ticket for the token family that authenticated
// this guard in the current request. Only its hash is stored. It expires after
// Config.TicketLifetime, the first redemption consumes it, and revoking the family
// deletes it. Mount it behind this binding's guard and deliver it through a
// no-store credential response; never put it in a URL.
func (t *Tokens[M, K]) IssueTicket(ctx context.Context) (Ticket[M, K], error) {
	if err := t.Validate(); err != nil {
		return Ticket[M, K]{}, err
	}
	backend, err := t.ticketBackend()
	if err != nil {
		return Ticket[M, K]{}, err
	}
	info, err := t.Current(ctx)
	if err != nil {
		return Ticket[M, K]{}, err
	}
	if info.Assurance() != auth.Authenticated {
		return Ticket[M, K]{}, auth.Unauthenticated
	}
	raw, hash, err := newSecret(t.store.config.Prefix)
	if err != nil {
		return Ticket[M, K]{}, err
	}
	var result Ticket[M, K]
	err = t.store.execute(ctx, func(op context.Context) error {
		identity, err := t.identity(info.Subject())
		if err != nil {
			return err
		}
		expires, err := backend.IssueTicket(op, t.address, identity, info.ID().value, hash, t.store.config.TicketLifetime, t.store.config.MaxTicketsPerFamily)
		if err != nil {
			return err
		}
		at, live := expires.Get()
		if !live {
			return auth.Unauthenticated
		}
		result = Ticket[M, K]{secret: raw, expires: at, family: info.ID()}
		return nil
	})
	if err != nil {
		return Ticket[M, K]{}, err
	}
	return result, nil
}

// TicketSource is the credential source that redeemed tickets authenticate:
// this binding's guard source.
func (t *Tokens[M, K]) TicketSource() auth.CredentialName { return t.Guard().Source() }

// RedeemTicket consumes a ticket issued by this binding. A redeemed ticket
// becomes a bound credential for TicketSource, which each later auth scope
// re-verifies against the live token family, its subject's current eligibility
// and the binding's scope ceiling. An unknown, expired or already redeemed
// ticket is omitted; the caller treats that as unauthenticated.
func (t *Tokens[M, K]) RedeemTicket(ctx context.Context, raw secret.String) (value.Optional[auth.BoundCredential], error) {
	if err := t.Validate(); err != nil {
		return value.Optional[auth.BoundCredential]{}, err
	}
	backend, err := t.ticketBackend()
	if err != nil {
		return value.Optional[auth.BoundCredential]{}, err
	}
	hash, err := HashSecret(raw)
	if err != nil {
		return value.Optional[auth.BoundCredential]{}, err
	}
	var result value.Optional[auth.BoundCredential]
	err = t.store.execute(ctx, func(op context.Context) error {
		found, err := backend.RedeemTicket(op, t.address, hash)
		if err != nil {
			return err
		}
		ticket, present := found.Get()
		if !present {
			return nil
		}
		if ticket.Family.IsZero() {
			return fault.New(fault.Invalid, "token backend returned an invalid ticket")
		}
		if _, err := t.provider.Parse(ticket.Subject); err != nil {
			return err
		}
		bound, err := t.tickets.Bind(familyBinding{family: ticket.Family, subject: ticket.Subject})
		if err != nil {
			return err
		}
		result = value.Set(bound)
		return nil
	})
	if err != nil {
		return value.Optional[auth.BoundCredential]{}, err
	}
	return result, nil
}

// verifyFamily re-checks a redeemed ticket's family for one auth scope. The
// family must still be live; its current generation supplies the grant, which
// the binding's ceiling narrows exactly as for an access token.
func (t *Tokens[M, K]) verifyFamily(ctx context.Context, binding familyBinding) (value.Optional[auth.Proof[M, K]], error) {
	backend, ok := t.store.backend.(TicketBackend)
	if !ok {
		return value.Optional[auth.Proof[M, K]]{}, auth.Unauthenticated
	}
	var result value.Optional[auth.Proof[M, K]]
	err := t.store.execute(ctx, func(op context.Context) error {
		found, err := backend.LookupFamily(op, t.address, binding.subject, binding.family)
		if err != nil {
			return err
		}
		record, present := found.Get()
		if !present {
			return auth.Unauthenticated
		}
		if record.ID != binding.family || record.Subject != binding.subject {
			return fault.New(fault.Invalid, "token backend returned a different family")
		}
		if record.Assurance != auth.Authenticated {
			return auth.Unauthenticated
		}
		info, err := t.info(record)
		if err != nil {
			return err
		}
		proof, err := auth.NewScopedProof(info.Subject(), info.Assurance(), info.Scopes())
		if err != nil {
			return err
		}
		proof, err = auth.AttachCredential(proof, t.current, info)
		if err != nil {
			return err
		}
		result = value.Set(proof)
		return nil
	})
	if err != nil {
		return value.Optional[auth.Proof[M, K]]{}, err
	}
	return result, nil
}
