package token

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Backend is an authoritative hash-only store. Every mutation owns one transaction
// and runs without automatic retries. Stable subject locks serialize creation,
// revocation and refresh. Sample server time after the relevant lock is acquired.
// Lookup only accepts the family's current access generation. Refresh atomically
// consumes the current generation and creates its successor, preserving the family
// lifetime and grant. A consumed refresh hash revokes the entire family, committing
// that revocation before returning an omitted result. Never roll it back merely
// because the public refresh operation is rejected. At the rotation limit, revoke
// rather than accumulate unbounded history. Retain consumed generations until the
// family expires or is revoked. Concurrent use may yield one success followed by
// family revocation; no replay grace period is implied. An unknown commit outcome
// returns an error, never a partial record or new secret.
type Backend interface {
	Create(context.Context, Address, Creation) (Record, error)
	Lookup(context.Context, Address, Digest, bool) (value.Optional[Record], error)
	Refresh(context.Context, Address, Digest, Digest, Digest) (value.Optional[Record], error)
	Revoke(context.Context, Address, Digest) (bool, error)
	RevokeID(context.Context, Address, model.Identity, model.ID[Record]) (bool, error)
	RevokeAll(context.Context, Address, model.Identity) (uint64, error)
	List(context.Context, Address, model.Identity, int) ([]Record, error)
	Prune(context.Context, Address, int) (uint64, error)
}

// GraceBackend additionally accepts the generation immediately before the
// family's current one, with Record.SupersededAt set, while the backend's clock
// is within grace of the successor's issue time and before its own access
// expiry. It never accepts older generations, and refresh-token reuse still
// revokes the family.
type GraceBackend interface {
	LookupWithin(context.Context, Address, Digest, time.Duration) (value.Optional[Record], error)
}

// TicketBackend stores single-use handshake tickets as hashes. IssueTicket
// stores one for the subject's live family, first dropping that family's expired
// tickets and the oldest beyond max-1, and reports omitted expiry when the
// family is no longer live. RedeemTicket deletes the ticket in the statement
// that reads it, so a ticket redeems at most once; an unknown or expired ticket
// is omitted. Revoking, refreshing past or expiring a family never revives a
// ticket, and deleting a family deletes its tickets. LookupFamily reads a family's
// current generation for a re-check and is omitted unless the family is live.
// PruneTickets deletes at most limit expired tickets of the address.
type TicketBackend interface {
	IssueTicket(context.Context, Address, model.Identity, model.ID[Record], Digest, time.Duration, int) (value.Optional[temporal.DateTime], error)
	RedeemTicket(context.Context, Address, Digest) (value.Optional[TicketRecord], error)
	LookupFamily(context.Context, Address, model.Identity, model.ID[Record]) (value.Optional[Record], error)
	PruneTickets(context.Context, Address, int) (uint64, error)
}

// TicketRecord is a redeemed ticket: the family and subject it was issued for.
type TicketRecord struct {
	Family    model.ID[Record]
	Subject   model.Identity
	ExpiresAt temporal.DateTime
}

// SelectiveBackend revokes every family of a subject in this address except
// keep, under the subject lock shared with issuance, and returns the count.
type SelectiveBackend interface {
	RevokeOthers(context.Context, Address, model.Identity, model.ID[Record]) (uint64, error)
}

// CheckedBackend verifies a proof's current model in the SAME transaction as
// creation, before acquiring credential subject locks. The check must lock the
// model and may run once only. A failed check creates nothing; never retry it.
type CheckedBackend interface {
	CreateChecked(context.Context, Address, Creation, func(context.Context, *database.Tx) error) (Record, error)
}

// TransactionalBackend joins a caller-owned transaction. It must verify pool
// ownership, restore schema state, preserve rollback, and never commit or retry.
type TransactionalBackend interface {
	RevokeAllIn(context.Context, *database.Tx, Address, model.Identity) (uint64, error)
}

// CompletionBackend can replace a pending-MFA credential in checked creation.
// ConsumePendingIn joins the supplied transaction, locks and revalidates the
// exact pending record/hash/identity/expiry, and deletes it provisionally. It
// rejects full credentials. CreateBefore runs the check and creates through the
// ordinary creation path, then rechecks the pending deadline before commit.
// Both methods preserve rollback, never retry, and return no secret on failure.
type CompletionBackend interface {
	CheckedBackend
	ConsumePendingIn(context.Context, *database.Tx, Address, Record) error
	CreateBefore(context.Context, Address, Creation, temporal.DateTime, func(context.Context, *database.Tx) error) (Record, error)
}
