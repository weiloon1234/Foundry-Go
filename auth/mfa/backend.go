package mfa

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Backend owns one read-committed transaction. Within invokes prepare exactly
// once to lock the application model BEFORE reading/locking the factor record.
// That model lock serializes creation even when no factor row exists. It then
// calls change once with owned state and server time, validates its result and
// deadline, persists it and commits. Callback errors, panic/Goexit or cancellation
// roll back all database changes. Never suppress failures, detach callbacks,
// retry mutations or acquire model locks after factor/credential locks.
//
// Callbacks use only the supplied transaction for database writes. The runtime
// may consult its bounded MFA lockout store before protected writes; external
// effects (email, publishing) require an outbox. After-commit failure may retain
// a Committed database outcome; an error is not permission to retry blindly.
// Prune deletes only expired pending factors and acquires no model locks.
type Backend interface {
	// Within prepares the model lock, reads the factor and applies one change in an owned
	// transaction without retries.
	Within(context.Context, Address, model.Identity, func(context.Context, *database.Tx) error, func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error)
	// Prune deletes at most limit expired pending factors without acquiring model locks.
	Prune(context.Context, Address, int) (uint64, error)
}

// Change explicitly selects replacement or removal. Zero is invalid. Before
// adds a strict pre-commit deadline, used to prevent late enrollment confirmation.
type Change struct {
	kind   uint8
	record Record
	until  value.Optional[temporal.DateTime]
}

// Replace selects replacement with an owned clone of record.
func Replace(record Record) Change { return Change{kind: 1, record: record.Clone()} }

// Remove selects deletion of the current factor record.
func Remove() Change { return Change{kind: 2} }

// Before returns a change with a strict deadline to recheck before commit.
func (c Change) Before(until temporal.DateTime) Change { c.until = value.Set(until); return c }

// Deadline returns the optional pre-commit deadline.
func (c Change) Deadline() value.Optional[temporal.DateTime] { return c.until }

// Removes reports whether the change selects deletion.
func (c Change) Removes() bool { return c.kind == 2 }

// Next returns an owned clone of the replacement record, or omission for a removal or
// undefined change.
func (c Change) Next() value.Optional[Record] {
	if c.kind != 1 {
		return value.Optional[Record]{}
	}
	return value.Set(c.record.Clone())
}

// Validate checks the mutation choice, optional deadline and replacement's address and
// subject ownership.
func (c Change) Validate(address Address, subject model.Identity) error {
	if c.kind != 1 && c.kind != 2 {
		return safeerror.NewFault(fault.Invalid, "MFA mutation must select replacement or removal")
	}
	if until, present := c.until.Get(); present && !validInstant(until) {
		return safeerror.NewFault(fault.Invalid, "invalid MFA mutation deadline")
	}
	if c.kind == 1 {
		return c.record.Validate(address, subject)
	}
	return nil
}

// TransactionalBackend joins the credential creation transaction. It verifies
// exact pool ownership, uses a savepoint, restores schema settings and never
// commits. The same model-before-factor ordering and callback rules apply.
type TransactionalBackend interface {
	// WithinIn applies the model-before-factor mutation contract within the caller-owned
	// transaction, without committing it.
	WithinIn(context.Context, *database.Tx, Address, model.Identity, func(context.Context, *database.Tx) error, func(context.Context, *database.Tx, value.Optional[Record], temporal.DateTime) (Change, error)) (value.Optional[Record], error)
}
