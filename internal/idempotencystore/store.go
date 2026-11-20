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
	return Records.Query().Where(orm.Eq(f.Namespace, a.Namespace), orm.Eq(f.Operation, a.Operation), orm.Eq(f.Version, a.Version), orm.Eq(f.ScopeDigest, a.Scope), orm.Eq(f.KeyDigest, a.Key))
}

// Claim uses one INSERT with a unique arbiter. A skipped insert must be followed
// by a fresh statement at Read Committed, never interpreted as a missing row.
func Claim(ctx context.Context, tx *database.Tx, a Address, fingerprint string) (value.Optional[Record], error) {
	f := Records
	return orm.Upsert(ctx, tx, Records.Query(), orm.OnConflict(orm.ConflictKey(f.Namespace), orm.ConflictKey(f.Operation), orm.ConflictKey(f.Version), orm.ConflictKey(f.ScopeDigest), orm.ConflictKey(f.KeyDigest)).DoNothing(), orm.Set(Records.Namespace, a.Namespace), orm.Set(Records.Operation, a.Operation), orm.Set(Records.Version, a.Version), orm.Set(Records.ScopeDigest, a.Scope), orm.Set(Records.KeyDigest, a.Key), orm.Set(Records.Fingerprint, fingerprint), orm.SetNull(Records.CompletedAt), orm.SetNull(Records.ExpiresAt))
}
func Find(ctx context.Context, tx *database.Tx, a Address) (value.Optional[Record], error) {
	return orm.First(ctx, tx, matching(a))
}

// CountRetained counts the caller's committed outcomes that are unexpired at
// now, stopping at limit. Uncommitted claims of other transactions are not
// visible; the caller's own claim has no expiry and is not counted. The index
// on (namespace, scope_digest, expires_at) bounds the read to limit rows.
func CountRetained(ctx context.Context, tx *database.Tx, a Address, now temporal.DateTime, limit int) (int64, error) {
	f := Records
	return orm.Count(ctx, tx, Records.Query().Where(orm.Eq(f.Namespace, a.Namespace), orm.Eq(f.ScopeDigest, a.Scope), orm.Gt(f.ExpiresAt, now)).Limit(limit))
}
func Complete(ctx context.Context, tx *database.Tx, row Record, schema, hash string, data []byte, completed, expires temporal.DateTime) error {
	_, err := orm.Update(ctx, tx, Records.Table.Query(), Records.Table.Key(), row.ID, orm.Set(Records.ResultSchema, schema), orm.Set(Records.ResultHash, hash), orm.Set(Records.Representation, data), orm.Set(Records.CompletedAt, completed), orm.Set(Records.ExpiresAt, expires))
	return err
}
