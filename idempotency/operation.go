package idempotency

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/idempotencystore"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Handler receives the transaction that owns claim, writes, outbox and outcome.
// It must not commit an independently captured pool or call external providers.
type Handler[I, R any] func(context.Context, *database.Tx, I) (R, error)
type Operation[I, R any] struct {
	store      *Store
	definition Definition
	input      Input[I]
	output     Encoding[R]
}

func Define[I, R any](store *Store, definition Definition, input Input[I], output Encoding[R]) (Operation[I, R], error) {
	for _, err := range []error{store.Validate(), definition.Validate(), input.Validate(), output.Validate()} {
		if err != nil {
			return Operation[I, R]{}, err
		}
	}
	return Operation[I, R]{store, definition, input, output}, nil
}
func (o Operation[I, R]) Validate() error {
	for _, err := range []error{o.store.Validate(), o.definition.Validate(), o.input.Validate(), o.output.Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
func (o Operation[I, R]) Definition() Definition { return o.definition }
func (Operation[I, R]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("typed idempotent operation"))
}

// Result owns the committed representation. A non-nil error with Committed()==true
// reports a post-commit operational failure: publish the result and report the
// failure, but never repeat the callback. Values/bytes are never formatted.
type Result[R any] struct {
	value               R
	encoded             []byte
	replayed, committed bool
}

func (r Result[R]) Value() R                 { return r.value }
func (r Result[R]) Encoded() []byte          { return slices.Clone(r.encoded) }
func (r Result[R]) Replayed() bool           { return r.replayed }
func (r Result[R]) Committed() bool          { return r.committed }
func (Result[R]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotent operation result")) }

// Run never retries the business callback. An uncertain commit is reconciled on
// the primary, or becomes Unavailable. Retry with the SAME scope/key and input.
// Authorization belongs before every Run, including those that only replay.
// Exhausted local admission (fault.Overloaded) returns a retryable Unavailable.
func (o Operation[I, R]) Run(ctx context.Context, scope Scope, key Key, input I, handler Handler[I, R]) (Result[R], error) {
	if err := o.Validate(); err != nil {
		return Result[R]{}, err
	}
	if ctx == nil || handler == nil || scope.digest == "" {
		return Result[R]{}, invalid("operation requires context, trusted scope and handler")
	}
	if _, err := ParseKey(key.text); err != nil || len(key.text) > o.store.config.MaxKeyBytes {
		return Result[R]{}, BadKey
	}
	lease, err := o.store.calls.Begin(ctx)
	if err != nil {
		// The operation never started; the same key can be retried shortly.
		if errors.Is(err, fault.Overloaded) {
			return Result[R]{}, failure(Unavailable, err)
		}
		return Result[R]{}, err
	}
	defer lease.Release()
	// Framework code runs in this goroutine. Application input/result codecs and
	// the transaction callback (which runs the handler) are already isolated by
	// their own owners, so Goexit cannot bypass rollback or release.
	var result Result[R]
	err = callback.Invoke("idempotent operation", func() error {
		var err error
		result, err = o.run(lease.Context(), scope, key, input, handler)
		return err
	})
	return result, err
}
func (o Operation[I, R]) run(ctx context.Context, scope Scope, key Key, input I, handler Handler[I, R]) (Result[R], error) {
	data, err := o.input.prepare(ctx, input, o.store.config.MaxInputBytes)
	if err != nil {
		return Result[R]{}, err
	}
	canonical, err := jsonwire.Canonical(data, jsonwire.Limits{Bytes: o.store.config.MaxInputBytes, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes})
	if err != nil {
		return Result[R]{}, err
	}
	fingerprint := digest("foundry.idempotency.input.v1", string(o.definition.ID), strconv.FormatUint(uint64(o.definition.Version), 10), o.input.identity, string(canonical))
	address := idempotencystore.Address{Namespace: o.store.namespaceDigest(), Operation: string(o.definition.ID), Version: o.definition.Version, Scope: scope.digest, Key: digest("foundry.idempotency.key.v1", key.text)}
	var result Result[R]
	err = o.store.db.Transaction(ctx, func(tx *database.Tx) error {
		previous, err := o.store.enter(ctx, tx)
		if err != nil {
			return err
		}
		inserted, err := idempotencystore.Claim(ctx, tx, address, fingerprint)
		if err != nil {
			// Only the claim waits on a lock bounded by DuplicateWait: the
			// uncommitted winner of this exact address. Other lock timeouts
			// never report an in-progress duplicate.
			if lockTimeout(err) {
				return failure(InProgress, err)
			}
			return err
		}
		row, claimed := inserted.Get()
		if !claimed {
			// A fresh Read Committed statement observes the committed winner.
			stored, err := idempotencystore.Find(ctx, tx, address)
			if err != nil {
				return err
			}
			row, exists := stored.Get()
			if !exists {
				return Unavailable
			}
			result, err = o.restore(ctx, row, fingerprint)
			return err
		}
		if err := o.admit(ctx, tx, address); err != nil {
			return err
		}
		if err := o.store.leave(ctx, tx, previous); err != nil {
			return err
		}
		value, err := handler(ctx, tx, input)
		if err != nil {
			return err
		}
		encoded, err := o.output.prepare(ctx, value, o.store.config.MaxResultBytes)
		if err != nil {
			return err
		}
		// Prove this exact representation can be restored before committing effects.
		restored, err := o.output.restore(ctx, encoded, o.store.config.MaxResultBytes)
		if err != nil {
			return err
		}
		completed, err := temporal.Now(o.store.db.Clock())
		if err != nil {
			return err
		}
		if completed.IsZero() {
			return invalid("idempotency clock returned no completion instant")
		}
		expires, err := completed.Add(o.store.config.Retention)
		if err != nil {
			return err
		}
		// Completion is the transaction's last statement, so the schema scope
		// ends at commit without a separate restoration round trip.
		if err := o.store.selectSchema(ctx, tx); err != nil {
			return err
		}
		if err := idempotencystore.Complete(ctx, tx, row, o.output.identity, digest("foundry.idempotency.result.v1", string(encoded)), encoded, completed, expires); err != nil {
			return err
		}
		result = Result[R]{value: restored, encoded: encoded}
		return nil
	}, database.TxOptions{Isolation: database.ReadCommitted})
	if err == nil {
		result.committed = true
		return result, nil
	}
	// Failure classification inspects arbitrary handler errors; isolate it.
	var outcome database.Outcome
	var inProgress, locked bool
	if failed := callback.Isolated("classify idempotent operation failure", func() error {
		if dbError, found, _ := errorgraph.As[*database.Error](err); found && dbError != nil {
			outcome = dbError.Outcome()
		}
		inProgress = errorgraph.Is(err, InProgress)
		locked = lockTimeout(err)
		return nil
	}); failed != nil {
		return Result[R]{}, failure(Unavailable, errors.Join(err, failed))
	}
	switch {
	case outcome == database.Committed:
		result.committed = true
		return result, err
	case outcome == database.Unknown:
		return o.reconcile(ctx, address, fingerprint, err)
	case inProgress:
		return Result[R]{}, err
	case locked:
		// An application statement's own lock wait expired; the transaction
		// rolled back, so the same key can be retried.
		return Result[R]{}, failure(Unavailable, err)
	}
	return Result[R]{}, err
}

// admit enforces the caller's retention quota after this request's claim. It
// reads at most MaxRetainedPerCaller unexpired committed outcomes and takes no
// lock, so a caller's operations never serialize on each other.
func (o Operation[I, R]) admit(ctx context.Context, tx *database.Tx, address idempotencystore.Address) error {
	now, err := temporal.Now(o.store.db.Clock())
	if err != nil {
		return err
	}
	retained, err := idempotencystore.CountRetained(ctx, tx, address, now, o.store.config.MaxRetainedPerCaller)
	if err != nil {
		return err
	}
	// The uncommitted claim is this request's own retained record.
	if retained >= int64(o.store.config.MaxRetainedPerCaller) {
		return Capacity
	}
	return nil
}

// lockTimeout reports a lock_timeout/NOWAIT expiry within the bounded graph.
func lockTimeout(err error) bool {
	return errorgraph.Is(err, database.LockNotAvailable)
}

// restore replays a committed outcome. A stored result schema identity that
// differs from the current encoding (a changed response contract) replays only
// when the current codec still accepts the exact stored representation, which
// preserves compatible schema evolution. An incompatible outcome stays
// Unavailable: re-executing a committed operation could duplicate its effects.
func (o Operation[I, R]) restore(ctx context.Context, row idempotencystore.Record, fingerprint string) (Result[R], error) {
	if row.Fingerprint != fingerprint {
		return Result[R]{}, Mismatch
	}
	if len(row.Representation) == 0 || len(row.Representation) > o.store.config.MaxResultBytes {
		return Result[R]{}, Unavailable
	}
	completed, complete := row.CompletedAt.Get()
	expires, expiring := row.ExpiresAt.Get()
	if !complete || !expiring || completed.IsZero() || !expires.UTC().After(completed.UTC()) || !identityPart(row.ResultSchema) || row.ResultHash != digest("foundry.idempotency.result.v1", string(row.Representation)) {
		return Result[R]{}, Unavailable
	}
	value, err := o.output.restore(ctx, row.Representation, o.store.config.MaxResultBytes)
	if err != nil {
		return Result[R]{}, failure(Unavailable, err)
	}
	return Result[R]{value: value, encoded: slices.Clone(row.Representation), replayed: true}, nil
}
func (o Operation[I, R]) reconcile(ctx context.Context, address idempotencystore.Address, fingerprint string, cause error) (Result[R], error) {
	var result Result[R]
	err := o.store.db.Transaction(ctx, func(tx *database.Tx) error {
		if err := o.store.selectSchema(ctx, tx); err != nil {
			return err
		}
		stored, err := idempotencystore.Find(ctx, tx, address)
		if err != nil {
			return err
		}
		row, exists := stored.Get()
		if !exists {
			return Unavailable
		}
		result, err = o.restore(ctx, row, fingerprint)
		return err
	}, database.TxOptions{Isolation: database.ReadCommitted, ReadOnly: true})
	if err != nil {
		return Result[R]{}, failure(Unavailable, errors.Join(cause, err))
	}
	result.committed = true
	// The caller receives the resolved outcome and still reports the lost commit
	// acknowledgment as an operational failure, rather than concealing it.
	return result, cause
}
