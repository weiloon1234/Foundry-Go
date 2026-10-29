package auditstore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Append requires already validated, captured input. Normal declaration writes
// retain savepoint behavior and never commit the supplied outer transaction.
func Append(ctx context.Context, tx *database.Tx, draft []orm.Assignment[Entry]) (Entry, error) {
	return orm.Insert(ctx, tx, Entries.Table, draft...)
}

// Prune selects a bounded oldest window and removes only that window through
// the existing typed set-based write compiler. No payloads are hydrated and no
// per-model observers run. A concurrent pruner may remove fewer rows.
func Prune(ctx context.Context, tx *database.Tx, area string, cutoff temporal.DateTime, limit int) (int64, error) {
	f := Entries
	destination := Entries.Query().Where(orm.TextEq(f.Area, area), orm.TimeLt(f.CreatedAt, cutoff))
	source := destination.OrderBy(orm.TimeAsc(f.CreatedAt), orm.NumberAsc(f.Sequence)).Limit(limit)
	return rows.DeleteUsing(destination, rows.From(source)).Match(rows.MatchSource(orm.KeyExpr(f.ID), expr.Value(orm.KeyExpr(f.ID)))).Exec(ctx, tx)
}

// CountBefore counts the entries Prune would remove for area and cutoff without
// reading payloads. It uses the same predicate as Prune.
func CountBefore(ctx context.Context, executor database.Executor, area string, cutoff temporal.DateTime) (int64, error) {
	f := Entries
	return orm.Count(ctx, executor, Entries.Query().Where(orm.TextEq(f.Area, area), orm.TimeLt(f.CreatedAt, cutoff)))
}

// Page retains the ordinary query layer's count/snapshot and row-limit contract.
// Rows are newest first by database insertion sequence, so writes made in one
// transaction keep their actual order despite sharing a transaction timestamp.
func Page(ctx context.Context, executor database.Executor, q orm.Query[Entry], request orm.PageRequest) (orm.PageResult[Entry], error) {
	f := Entries
	return orm.Paginate(ctx, executor, q.OrderBy(orm.NumberDesc(f.Sequence)), request)
}

// Recent reads at most limit complete rows newest first, strictly before the
// supplied sequence when one is given. It is the keyset step of typed history.
func Recent(ctx context.Context, executor database.Executor, q orm.Query[Entry], before int64, limit int) ([]Entry, error) {
	f := Entries
	if before > 0 {
		q = q.Where(orm.NumberLt(f.Sequence, before))
	}
	return orm.All(ctx, executor, q.OrderBy(orm.NumberDesc(f.Sequence)).Limit(limit))
}

// RecentActivity is Recent without selecting or decoding stored payloads.
func RecentActivity(ctx context.Context, executor database.Executor, q orm.Query[Entry], before int64, limit int) ([]Activity, error) {
	f := Entries
	if before > 0 {
		q = q.Where(orm.NumberLt(f.Sequence, before))
	}
	return rows.Read(Activities(q.OrderBy(orm.NumberDesc(f.Sequence)).Limit(limit))).All(ctx, executor)
}
