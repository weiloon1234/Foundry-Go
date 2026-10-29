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
	return Records.Query().Where(orm.TextEq(f.Namespace, a.Namespace), orm.TextEq(f.Operation, a.Operation), orm.NumberEq(f.Version, a.Version), orm.TextEq(f.ScopeDigest, a.Scope), orm.TextEq(f.KeyDigest, a.Key))
}

// Claim uses one INSERT with a unique arbiter. A skipped insert must be followed
// by a fresh statement at Read Committed, never interpreted as a missing row.
func Claim(ctx context.Context, tx *database.Tx, a Address, fingerprint string) (value.Optional[Record], error) {
	f := Records
	return orm.Upsert(ctx, tx, Records.Query(), orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Namespace)), orm.ConflictKey(orm.TextExpr(f.Operation)), orm.ConflictKey(orm.NumberExpr(f.Version)), orm.ConflictKey(orm.TextExpr(f.ScopeDigest)), orm.ConflictKey(orm.TextExpr(f.KeyDigest))).DoNothing(), orm.TextSet(Records.Namespace, a.Namespace), orm.TextSet(Records.Operation, a.Operation), orm.NumberSet(Records.Version, a.Version), orm.TextSet(Records.ScopeDigest, a.Scope), orm.TextSet(Records.KeyDigest, a.Key), orm.TextSet(Records.Fingerprint, fingerprint), orm.NullableTimeSetNull(Records.CompletedAt), orm.NullableTimeSetNull(Records.ExpiresAt))
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
	return orm.Count(ctx, tx, Records.Query().Where(orm.TextEq(f.Namespace, a.Namespace), orm.TextEq(f.ScopeDigest, a.Scope), orm.NullableTimeGt(f.ExpiresAt, now)).Limit(limit))
}
func Complete(ctx context.Context, tx *database.Tx, row Record, schema, hash string, data []byte, completed, expires temporal.DateTime) error {
	_, err := orm.Update(ctx, tx, Records.Table.Query(), Records.Table.Key(), row.ID, orm.TextSet(Records.ResultSchema, schema), orm.TextSet(Records.ResultHash, hash), orm.ColumnSet(Records.Representation, data), orm.NullableTimeSet(Records.CompletedAt, completed), orm.NullableTimeSet(Records.ExpiresAt, expires))
	return err
}
