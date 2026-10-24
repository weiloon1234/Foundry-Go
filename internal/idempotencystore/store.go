package idempotencystore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Address struct {
	Namespace, Operation string
	Version              uint32
	Scope, Key           string
}

func matching(a Address) orm.Query[Record] {
	f := Records
	return Records.Query().Where(f.Namespace.Eq(a.Namespace), f.Operation.Eq(a.Operation), f.Version.Eq(a.Version), f.ScopeDigest.Eq(a.Scope), f.KeyDigest.Eq(a.Key))
}

// Claim uses one INSERT with a unique arbiter. A skipped insert must be followed
// by a fresh statement at Read Committed, never interpreted as a missing row.
func Claim(ctx context.Context, tx *database.Tx, a Address, fingerprint string) (value.Optional[Record], error) {
	f := Records
	return orm.Upsert(ctx, tx, Records.Query(), orm.OnConflict(orm.ConflictKey(f.Namespace), orm.ConflictKey(f.Operation), orm.ConflictKey(f.Version), orm.ConflictKey(f.ScopeDigest), orm.ConflictKey(f.KeyDigest)).DoNothing(), Records.Namespace.Set(a.Namespace), Records.Operation.Set(a.Operation), Records.Version.Set(a.Version), Records.ScopeDigest.Set(a.Scope), Records.KeyDigest.Set(a.Key), Records.Fingerprint.Set(fingerprint), Records.CompletedAt.SetNull(), Records.ExpiresAt.SetNull())
}
func Find(ctx context.Context, tx *database.Tx, a Address) (value.Optional[Record], error) {
	return matching(a).First(ctx, tx)
}

// CountRetained counts the caller's committed outcomes that are unexpired at
// now, stopping at limit. Uncommitted claims of other transactions are not
// visible; the caller's own claim has no expiry and is not counted. The index
// on (namespace, scope_digest, expires_at) bounds the read to limit rows.
func CountRetained(ctx context.Context, tx *database.Tx, a Address, now temporal.DateTime, limit int) (int64, error) {
	f := Records
	return Records.Query().Where(f.Namespace.Eq(a.Namespace), f.ScopeDigest.Eq(a.Scope), f.ExpiresAt.Gt(now)).Limit(limit).Count(ctx, tx)
}
func Complete(ctx context.Context, tx *database.Tx, row Record, schema, hash string, data []byte, completed, expires temporal.DateTime) error {
	_, err := Records.Update(ctx, tx, row.ID, Records.ResultSchema.Set(schema), Records.ResultHash.Set(hash), Records.Representation.Set(data), Records.CompletedAt.Set(completed), Records.ExpiresAt.Set(expires))
	return err
}
