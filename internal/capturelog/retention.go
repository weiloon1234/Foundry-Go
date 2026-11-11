package capturelog

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	pruneInterval = 10 * time.Minute
	pruneCatchUp  = time.Minute
	// PruneBatch is the most rows one retention transaction removes.
	PruneBatch = 1_000
	// PruneBatches bounds one scheduled pass.
	PruneBatches = 50
	pruneTimeout = 5 * time.Second
)

// ErrRetentionBusy reports that another process holds this table's retention
// lock for the schema; the batch was skipped.
var ErrRetentionBusy = safeerror.NewFault(fault.Conflict, "another process holds the capture retention lock")

// PrunePass runs one scheduled pass and reports whether it stopped at its
// batch bound, so the next pass comes sooner. The first pass also checks the
// table, which logs once when it is not migrated.
func (l *Log[S, M]) PrunePass(ctx context.Context) bool {
	l.mu.Lock()
	if !l.checked {
		l.checkLocked(ctx)
	}
	l.mu.Unlock()
	cutoff, err := l.Cutoff()
	if err != nil {
		return false
	}
	_, behind, err := l.Prune(ctx, cutoff, true, PruneBatch, PruneBatches)
	if err != nil && !errors.Is(err, ErrRetentionBusy) && ctx.Err() == nil {
		l.logger.Warn(l.table.Label+" retention pass failed", "schema", l.schema)
	}
	return behind
}

// Prune deletes rows captured before cutoff, then, when capped, rows beyond
// MaxRows (per partition value when the table has one), oldest first. Every
// batch is one transaction holding the transaction-scoped retention lock; a
// process that loses it stops with ErrRetentionBusy. It reports whether it
// stopped at maxBatches.
func (l *Log[S, M]) Prune(ctx context.Context, cutoff temporal.DateTime, capped bool, batch, maxBatches int) (int64, bool, error) {
	t := l.table
	var removed int64
	batches := 0
	run := func(scope orm.Query[M], before temporal.DateTime) (bool, error) {
		for batches < maxBatches {
			n, err := l.pruneBatch(ctx, func(ctx context.Context, tx *database.Tx) (int64, error) {
				return deleteBefore(ctx, tx, t, scope, before, batch)
			})
			if err != nil {
				return false, err
			}
			removed += n
			batches++
			if n < int64(batch) {
				return true, nil
			}
		}
		return false, nil
	}
	done, err := run(t.Query(), cutoff)
	if err != nil || !done || !capped {
		return removed, !done && err == nil, err
	}
	capScope := func(scope orm.Query[M]) (bool, error) {
		var boundary temporal.DateTime
		var over bool
		if _, err := l.pruneBatch(ctx, func(ctx context.Context, tx *database.Tx) (int64, error) {
			newest, err := overflow(ctx, tx, t, scope, l.limits.MaxRows)
			boundary, over = newest.Get()
			return 0, err
		}); err != nil || !over {
			return err == nil, err
		}
		after, err := boundary.Add(time.Microsecond)
		if err != nil {
			return false, err
		}
		return run(scope, after)
	}
	if t.Partition == nil {
		done, err = capScope(t.Query())
		return removed, !done && err == nil, err
	}
	// Keyset over the distinct partition values, one index seek each, so no
	// value is ever skipped however many there are.
	var last value.Optional[string]
	for {
		var next value.Optional[string]
		if _, err := l.pruneBatch(ctx, func(ctx context.Context, tx *database.Tx) (int64, error) {
			var err error
			next, err = nextPartition(ctx, tx, t, last)
			return 0, err
		}); err != nil {
			return removed, false, err
		}
		partition, more := next.Get()
		if !more {
			return removed, false, nil
		}
		if done, err = capScope(t.Query().Where(orm.TextEq(*t.Partition, partition))); err != nil || !done {
			return removed, !done && err == nil, err
		}
		last = next
	}
}

func (l *Log[S, M]) pruneBatch(ctx context.Context, fn func(context.Context, *database.Tx) (int64, error)) (int64, error) {
	bounded, cancel := context.WithTimeout(ctx, pruneTimeout)
	defer cancel()
	var removed int64
	err := l.Transaction(bounded, false, func(tx *database.Tx) error {
		locked, err := l.Lock(bounded, tx)
		if err != nil {
			return err
		}
		if !locked {
			return ErrRetentionBusy
		}
		removed, err = fn(bounded, tx)
		return err
	})
	return removed, err
}

// Lock takes the transaction-scoped retention lock for this table and schema
// without waiting; processes that lose the race skip the batch.
func (l *Log[S, M]) Lock(ctx context.Context, tx *database.Tx) (bool, error) {
	var acquired bool
	err := database.ScanOne(ctx, tx, `SELECT pg_catalog.pg_try_advisory_xact_lock(pg_catalog.hashtext($1), pg_catalog.hashtext($2))`, []any{l.table.Lock, l.schema}, &acquired)
	return acquired, err
}

// Estimate counts rows a prune would remove without deleting them.
func (l *Log[S, M]) Estimate(ctx context.Context, cutoff temporal.DateTime, capped bool) (int64, error) {
	t := l.table
	var total int64
	err := l.Transaction(ctx, true, func(tx *database.Tx) error {
		expired, err := orm.Count(ctx, tx, t.Query().Where(orm.TimeLt(t.At, cutoff)))
		total = expired
		if err != nil || !capped {
			return err
		}
		over := func(scope orm.Query[M]) error {
			retained, err := orm.Count(ctx, tx, scope.Where(orm.TimeGte(t.At, cutoff)))
			total += max(0, retained-int64(l.limits.MaxRows))
			return err
		}
		if t.Partition == nil {
			return over(t.Query())
		}
		var last value.Optional[string]
		for {
			next, err := nextPartition(ctx, tx, t, last)
			partition, more := next.Get()
			if err != nil || !more {
				return err
			}
			if err := over(t.Query().Where(orm.TextEq(*t.Partition, partition))); err != nil {
				return err
			}
			last = next
		}
	})
	return total, err
}

// deleteBefore removes up to limit rows of scope captured before cutoff,
// oldest first, in one statement that never loads payloads.
func deleteBefore[M any](ctx context.Context, tx *database.Tx, t Table[M], scope orm.Query[M], cutoff temporal.DateTime, limit int) (int64, error) {
	oldest := rows.SelectScalar(rows.From(scope.Where(orm.TimeLt(t.At, cutoff)).OrderBy(orm.TimeAsc(t.At), orm.KeyAsc(t.ID)).Limit(limit)), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(t.ID)))))
	return orm.DeleteAll(ctx, tx, expr.Where(t.Query(), rows.InQuery(expr.Value(orm.KeyExpr(t.ID)), oldest)))
}

// overflow returns the capture time of the newest row of scope beyond keep,
// when it holds more than keep rows. Rows captured at or before it exceed
// the cap; rows sharing that microsecond with a kept row are removed too.
func overflow[M any](ctx context.Context, tx *database.Tx, t Table[M], scope orm.Query[M], keep int) (value.Optional[temporal.DateTime], error) {
	return rows.Read(rows.SelectValue(scope.OrderBy(orm.TimeDesc(t.At), orm.KeyDesc(t.ID)).Offset(keep).Limit(1), orm.TimeExpr(t.At))).First(ctx, tx)
}

// nextPartition returns the smallest partition value after last.
func nextPartition[M any](ctx context.Context, tx *database.Tx, t Table[M], last value.Optional[string]) (value.Optional[string], error) {
	q := t.Query()
	if after, set := last.Get(); set {
		q = q.Where(orm.TextGt(*t.Partition, after))
	}
	return rows.Read(rows.SelectValue(q.OrderBy(orm.TextAsc(*t.Partition)).Limit(1), orm.TextExpr(*t.Partition))).First(ctx, tx)
}
