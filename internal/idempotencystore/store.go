package idempotencystore

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Address struct {
	Namespace, Operation string
	Version              uint32
	Scope, Key           string
}

func matching(a Address) RecordQuery {
	f := RecordFields()
	return QueryFoundryIdempotency().Where(f.Namespace.Eq(a.Namespace), f.Operation.Eq(a.Operation), f.Version.Eq(a.Version), f.ScopeDigest.Eq(a.Scope), f.KeyDigest.Eq(a.Key))
}

// Claim uses one INSERT with a unique arbiter. A skipped insert must be followed
// by a fresh statement at Read Committed, never interpreted as a missing row.
func Claim(ctx context.Context, tx *database.Tx, a Address, fingerprint string) (value.Optional[Record], error) {
	f := RecordFields()
	return QueryFoundryIdempotency().Upsert(ctx, tx, RecordDraft{}.SetNamespace(a.Namespace).SetOperation(a.Operation).SetVersion(a.Version).SetScopeDigest(a.Scope).SetKeyDigest(a.Key).SetFingerprint(fingerprint).ClearCompletedAt().ClearExpiresAt(), query.OnConflict(f.Namespace, f.Operation, f.Version, f.ScopeDigest, f.KeyDigest).DoNothing())
}
func Find(ctx context.Context, tx *database.Tx, a Address) (value.Optional[Record], error) {
	return matching(a).First(ctx, tx)
}

// CountRetained counts the caller's committed outcomes that are unexpired at
// now, stopping at limit. Uncommitted claims of other transactions are not
// visible; the caller's own claim has no expiry and is not counted. The index
// on (namespace, scope_digest, expires_at) bounds the read to limit rows.
func CountRetained(ctx context.Context, tx *database.Tx, a Address, now temporal.DateTime, limit int) (int64, error) {
	f := RecordFields()
	return QueryFoundryIdempotency().Where(f.Namespace.Eq(a.Namespace), f.ScopeDigest.Eq(a.Scope), f.ExpiresAt.Gt(now)).Limit(limit).Count(ctx, tx)
}
func Complete(ctx context.Context, tx *database.Tx, row Record, schema, hash string, data []byte, completed, expires temporal.DateTime) error {
	_, err := QueryFoundryIdempotency().Update(ctx, tx, row.ID, RecordDraft{}.SetResultSchema(schema).SetResultHash(hash).SetRepresentation(data).SetCompletedAt(completed).SetExpiresAt(expires))
	return err
}
