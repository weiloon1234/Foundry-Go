package challenge

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Backend owns a read-committed transaction for each operation, without retries.
// Issue locks the stable purpose/subject row, then invokes prepare exactly once
// in that transaction before replacing the previous token. Consume finds the
// hash, acquires the same subject lock, rechecks the current hash/expiry, invokes
// apply once, then removes the token in that SAME transaction. Before-commit
// errors/panic/Goexit/cancellation roll back model changes and consumption together.
// After-commit failures preserve a Committed database outcome; they cannot undo
// persistence. Never retry based only on the presence of an error.
// Callbacks must use only the supplied transaction for writes; no external I/O,
// detached work, nested challenge operations or additional pool acquisitions.
// Lock order is challenge subject, then application model. Stable subject rows
// remain after consumption. An uncertain commit returns an error and no result.
type Backend interface {
	// Issue locks the subject, prepares its binding and replaces the previous token in one
	// transaction without retries.
	Issue(context.Context, Address, model.Identity, Digest, time.Duration, func(context.Context, *database.Tx) (Binding, error)) (Record, error)
	// Consume locks and rechecks a live token, applies the action and removes the token in
	// the same transaction. An absent or expired token is omitted.
	Consume(context.Context, Address, Digest, func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error)
	// Revoke removes the subject's challenge and reports whether one existed.
	Revoke(context.Context, Address, model.Identity) (bool, error)
	// Prune removes up to the supplied limit of expired challenges in the address.
	Prune(context.Context, Address, int) (uint64, error)
}

// Record is a stored challenge's subject, binding, digest and lifetime.
type Record struct {
	Address   Address
	Subject   model.Identity
	Hash      Digest
	Binding   Binding
	CreatedAt temporal.DateTime
	ExpiresAt temporal.DateTime
}

// Format writes the label "stored challenge" without exposing the value.
func (Record) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("stored challenge")) }

// Validate checks address ownership, subject, hashes, timestamps and lifetime.
func (r Record) Validate(address Address) error {
	if _, err := address.SubjectKey(r.Subject); err != nil {
		return err
	}
	if r.Address != address || r.Hash.IsZero() || r.Binding.IsZero() || r.CreatedAt.IsZero() || r.ExpiresAt.IsZero() {
		return safeerror.NewFault(fault.Invalid, "invalid stored challenge")
	}
	return ValidateLifetime(r.ExpiresAt.UTC().Sub(r.CreatedAt.UTC()))
}

// Live reports whether now is at or after creation and strictly before expiry.
func (r Record) Live(now time.Time) bool {
	return !now.Before(r.CreatedAt.UTC()) && now.Before(r.ExpiresAt.UTC())
}
