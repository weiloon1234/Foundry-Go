package session

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Backend owns one authoritative session store. Every mutation is atomic and
// executes without automatic retry. RevokeAll serializes with creation; sessions
// issued after that operation are new credentials. Lookup/rotation cannot revive
// a removed record. Clock sampling occurs after acquiring the relevant lock.
// Errors may mean an unknown write outcome; they are not missing credentials.
// Adapters return complete validated records and enforce bounded list/prune work.
type Backend interface {
	// Create atomically creates a session under the subject's capacity policy.
	Create(context.Context, Address, Creation) (Record, error)
	// Lookup resolves a live session by digest, optionally recording activity.
	Lookup(context.Context, Address, Digest, bool) (value.Optional[Record], error)
	// Rotate replaces a live session's secret hash and records activity without reviving
	// removed records.
	Rotate(context.Context, Address, Digest, Digest) (value.Optional[Record], error)
	// Revoke removes the session identified by its credential digest and reports whether
	// it existed.
	Revoke(context.Context, Address, Digest) (bool, error)
	// RevokeID removes the subject's identified session and reports whether it existed.
	RevokeID(context.Context, Address, model.Identity, model.ID[Record]) (bool, error)
	// RevokeAll removes all of the subject's sessions, serializing with creation.
	RevokeAll(context.Context, Address, model.Identity) (uint64, error)
	// List returns a bounded page of the subject's session records.
	List(context.Context, Address, model.Identity, int) ([]Record, error)
	// Prune removes a bounded batch of expired sessions.
	Prune(context.Context, Address, int) (uint64, error)
}

// CheckedBackend verifies a proof's current model in the SAME transaction as
// creation, before acquiring credential subject locks. The check must lock the
// model and may run once only. A failed check creates nothing; never retry it.
type CheckedBackend interface {
	// CreateChecked runs the proof check before credential locks and creates the session
	// in the same transaction.
	CreateChecked(context.Context, Address, Creation, func(context.Context, *database.Tx) error) (Record, error)
}

// TransactionalBackend joins a caller-owned transaction. It must verify pool
// ownership, restore schema state, preserve rollback, and never commit or retry.
type TransactionalBackend interface {
	// RevokeAllIn removes the subject's sessions within the caller-owned transaction,
	// without committing or retrying.
	RevokeAllIn(context.Context, *database.Tx, Address, model.Identity) (uint64, error)
}

// DeadlineBackend performs checked creation and rechecks an existing
// credential's deadline before commit. Expiry rolls back all joined writes.
type DeadlineBackend interface {
	CheckedBackend
	// CreateBefore performs checked creation and rechecks the supplied deadline before
	// commit, rolling back joined writes on expiry.
	CreateBefore(context.Context, Address, Creation, temporal.DateTime, func(context.Context, *database.Tx) error) (Record, error)
}

// CompletionBackend can replace a pending-MFA credential in checked creation.
// ConsumePendingIn joins the supplied transaction, locks and revalidates the
// exact pending record/hash/identity/expiry, and deletes it provisionally. It
// rejects full credentials. CreateBefore runs the check and creates through the
// ordinary creation path, then rechecks the pending deadline before commit.
// Both methods preserve rollback, never retry, and return no secret on failure.
type CompletionBackend interface {
	DeadlineBackend
	// ConsumePendingIn locks, revalidates and provisionally deletes the exact pending-MFA
	// credential in the caller's transaction.
	ConsumePendingIn(context.Context, *database.Tx, Address, Record) error
}

// ResumptionBackend returns to the original actor's session after
// impersonation. ResumeActor runs check first, then, in the same transaction
// and under the subject lock, locks the exact live, fully authenticated,
// unimpersonated record (same hash, ID, subject, creation time and absolute
// expiry) and replaces its secret hash with next, recording activity. The ID,
// creation time, absolute expiry and remember policy never change. A revoked,
// rotated, expired or impersonated record returns an omitted value and
// changes nothing. It never retries.
type ResumptionBackend interface {
	// ResumeActor runs check and rotates the exact live original actor session in one
	// transaction, omitting revoked, rotated, expired or impersonated records.
	ResumeActor(context.Context, Address, Record, Digest, func(context.Context, *database.Tx) error) (value.Optional[Record], error)
}

// SelectiveBackend revokes every session of a subject in this address except
// keep, under the same subject lock as creation. It returns the removed count.
type SelectiveBackend interface {
	// RevokeOthers removes every session of the subject except keep, returning the count.
	RevokeOthers(context.Context, Address, model.Identity, model.ID[Record]) (uint64, error)
}

// ConfirmationBackend stamps ConfirmedAt on one live, fully authenticated
// session of the subject and returns the updated record, or omits it when the
// session is gone. ConfirmedWithin reports whether that live session confirmed
// within the duration of the backend's current time. Both sample its clock.
type ConfirmationBackend interface {
	// Confirm stamps confirmation on a live, fully authenticated session, omitting the
	// result when it is gone.
	Confirm(context.Context, Address, model.Identity, model.ID[Record]) (value.Optional[Record], error)
	// ConfirmedWithin checks whether the live session confirmed within the duration of the
	// authority's current time.
	ConfirmedWithin(context.Context, Address, model.Identity, model.ID[Record], time.Duration) (bool, error)
}
